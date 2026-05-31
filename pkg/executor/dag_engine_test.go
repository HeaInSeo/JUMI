package executor

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/HeaInSeo/JUMI/pkg/backend"
	"github.com/HeaInSeo/JUMI/pkg/handoff"
	"github.com/HeaInSeo/JUMI/pkg/provenance"
	"github.com/HeaInSeo/JUMI/pkg/registry"
	"github.com/HeaInSeo/JUMI/pkg/spec"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type fakePrepared struct {
	nodeID string
}

type fakeHandle struct {
	nodeID string
}

type fakeAdapter struct {
	mu                       sync.Mutex
	order                    []string
	failOn                   map[string]bool
	waitCh                   map[string]chan struct{}
	canceled                 map[string]bool
	prepared                 map[string]spec.Node
	outputs                  map[string]map[string]backend.OutputMetadata
	observe                  *backend.OptionalKueueInfo
	startDelay               time.Duration
	forceMetadataUnavailable bool
}

type fakeHandoffClient struct {
	mu               sync.Mutex
	requests         []handoff.ResolveBindingRequest
	registerRequests []handoff.RegisterArtifactRequest
	notifyRequests   []handoff.NotifyNodeTerminalRequest
	finalizeRequests []handoff.FinalizeSampleRunRequest
	evaluateRequests []handoff.EvaluateGCRequest
	registerHook     func(handoff.RegisterArtifactRequest)
	notifyHook       func(handoff.NotifyNodeTerminalRequest)
	response         handoff.ResolveBindingResponse
	responses        []handoff.ResolveBindingResponse
	resolveErr       error
	resolveErrs      []error
	registerErr      error
	notifyErr        error
	finalizeErr      error
	evaluateErr      error
}

func (f *fakeHandoffClient) ResolveBinding(_ context.Context, req handoff.ResolveBindingRequest) (handoff.ResolveBindingResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests = append(f.requests, req)
	if len(f.resolveErrs) > 0 {
		err := f.resolveErrs[0]
		f.resolveErrs = f.resolveErrs[1:]
		if err != nil {
			return handoff.ResolveBindingResponse{}, err
		}
	}
	if f.resolveErr != nil {
		return handoff.ResolveBindingResponse{}, f.resolveErr
	}
	if len(f.responses) > 0 {
		resp := f.responses[0]
		f.responses = f.responses[1:]
		return resp, nil
	}
	if f.response.ResolutionStatus == "" {
		return handoff.ResolveBindingResponse{
			ResolutionStatus: "RESOLVED",
			Decision:         "remote_fetch",
			PlacementIntent: handoff.PlacementIntent{
				Mode:     "required_node",
				NodeName: "node-a",
			},
			MaterializationPlan: handoff.MaterializationPlan{
				Mode:           "remote_fetch",
				URI:            "http://artifact.local/output",
				ExpectedDigest: "sha256:abc",
			},
		}, nil
	}
	return f.response, nil
}

func (f *fakeHandoffClient) RegisterArtifact(_ context.Context, req handoff.RegisterArtifactRequest) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.registerRequests = append(f.registerRequests, req)
	if f.registerHook != nil {
		f.registerHook(req)
	}
	return f.registerErr
}

func (f *fakeHandoffClient) NotifyNodeTerminal(_ context.Context, req handoff.NotifyNodeTerminalRequest) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.notifyRequests = append(f.notifyRequests, req)
	if f.notifyHook != nil {
		f.notifyHook(req)
	}
	return f.notifyErr
}

func (f *fakeHandoffClient) FinalizeSampleRun(_ context.Context, req handoff.FinalizeSampleRunRequest) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.finalizeRequests = append(f.finalizeRequests, req)
	return f.finalizeErr
}

func (f *fakeHandoffClient) EvaluateGC(_ context.Context, req handoff.EvaluateGCRequest) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.evaluateRequests = append(f.evaluateRequests, req)
	return f.evaluateErr
}

func (f *fakeHandoffClient) GetSampleRunLifecycle(_ context.Context, req handoff.GetSampleRunLifecycleRequest) (handoff.SampleRunLifecycle, bool, error) {
	return handoff.SampleRunLifecycle{SampleRunID: req.SampleRunID}, false, nil
}

func (f *fakeAdapter) PrepareNode(_ context.Context, _ spec.RunRecord, node spec.Node) (backend.PreparedNode, error) {
	f.mu.Lock()
	if f.prepared == nil {
		f.prepared = make(map[string]spec.Node)
	}
	f.prepared[node.NodeID] = node
	f.mu.Unlock()
	return fakePrepared{nodeID: node.NodeID}, nil
}

func TestDagEngineResolvesArtifactBindingsBeforeStart(t *testing.T) {
	reg := registry.NewMemoryRegistry()
	adapter := &fakeAdapter{failOn: map[string]bool{}}
	handoffClient := &fakeHandoffClient{
		response: handoff.ResolveBindingResponse{
			ResolutionStatus: "RESOLVED",
			Decision:         "remote_fetch",
			PlacementIntent:  handoff.PlacementIntent{Mode: "required_node", NodeName: "node-a"},
			MaterializationPlan: handoff.MaterializationPlan{
				Mode:           "remote_fetch",
				URI:            "http://artifact.local/output",
				ExpectedDigest: "sha256:abc",
			},
		},
	}
	engine := NewDagEngineWithHandoff(reg, adapter, handoffClient)
	specInput := spec.ExecutableRunSpec{
		Run: spec.RunMetadata{RunID: "run-bindings", SampleRunID: "sample-1", SubmittedAt: time.Now().UTC(), FailurePolicy: spec.FailurePolicy{Mode: "fail-fast"}},
		Graph: spec.Graph{
			Nodes: []spec.Node{
				{NodeID: "a", Image: "busybox:1.36"},
				{NodeID: "b", Image: "busybox:1.36", ArtifactBindings: []spec.ArtifactBinding{{
					BindingName:        "dataset",
					ChildInputName:     "dataset",
					ProducerNodeID:     "a",
					ProducerOutputName: "output",
					ArtifactID:         "sample-1:a:output",
					ConsumePolicy:      "RemoteOK",
					Required:           true,
				}}},
			},
			Edges: [][]string{{"a", "b"}},
		},
	}
	record := spec.RunRecord{RunID: specInput.Run.RunID, Status: spec.RunStatusAccepted, AcceptedAt: time.Now().UTC(), Spec: specInput}
	nodes := []spec.NodeRecord{{RunID: record.RunID, NodeID: "a", Status: spec.NodeStatusPending}, {RunID: record.RunID, NodeID: "b", Status: spec.NodeStatusPending}}
	if err := reg.CreateRun(context.Background(), record, nodes); err != nil {
		t.Fatalf("CreateRun() error = %v", err)
	}
	if err := engine.Admit(context.Background(), record); err != nil {
		t.Fatalf("Admit() error = %v", err)
	}
	waitForRunStatus(t, reg, record.RunID, spec.RunStatusSucceeded)
	handoffClient.mu.Lock()
	defer handoffClient.mu.Unlock()
	if len(handoffClient.requests) != 1 {
		t.Fatalf("resolve binding calls = %d, want 1", len(handoffClient.requests))
	}
	if handoffClient.requests[0].SampleRunID != "sample-1" {
		t.Fatalf("sampleRunID = %q, want sample-1", handoffClient.requests[0].SampleRunID)
	}
	if handoffClient.requests[0].ArtifactID != "sample-1:a:output" {
		t.Fatalf("artifactID = %q, want sample-1:a:output", handoffClient.requests[0].ArtifactID)
	}
	if handoffClient.requests[0].ProducerAttemptID == "" {
		t.Fatal("expected producerAttemptID to be populated")
	}
	if handoffClient.requests[0].ChildAttemptID == "" {
		t.Fatal("expected childAttemptID to be populated")
	}
	adapter.mu.Lock()
	preparedNode, ok := adapter.prepared["b"]
	adapter.mu.Unlock()
	if !ok {
		t.Fatal("expected prepared node for b")
	}
	if preparedNode.Env["JUMI_INPUT_DATASET_STATUS"] != "RESOLVED" {
		t.Fatalf("resolved status env = %q, want RESOLVED", preparedNode.Env["JUMI_INPUT_DATASET_STATUS"])
	}
	if preparedNode.Env["JUMI_INPUT_DATASET_DECISION"] != "remote_fetch" {
		t.Fatalf("resolved decision env = %q, want remote_fetch", preparedNode.Env["JUMI_INPUT_DATASET_DECISION"])
	}
	if preparedNode.Env["JUMI_INPUT_DATASET_SOURCE_NODE"] != "node-a" {
		t.Fatalf("source node env = %q, want node-a", preparedNode.Env["JUMI_INPUT_DATASET_SOURCE_NODE"])
	}
	if preparedNode.Env["JUMI_INPUT_DATASET_URI"] != "http://artifact.local/output" {
		t.Fatalf("uri env = %q, want http://artifact.local/output", preparedNode.Env["JUMI_INPUT_DATASET_URI"])
	}
	if preparedNode.Env["JUMI_INPUT_DATASET_PLACEMENT_MODE"] != "required_node" {
		t.Fatalf("placement mode env = %q, want required_node", preparedNode.Env["JUMI_INPUT_DATASET_PLACEMENT_MODE"])
	}
	if preparedNode.Env["JUMI_INPUT_DATASET_MATERIALIZATION_MODE"] != "remote_fetch" {
		t.Fatalf("materialization mode env = %q, want remote_fetch", preparedNode.Env["JUMI_INPUT_DATASET_MATERIALIZATION_MODE"])
	}
	if preparedNode.Env["JUMI_INPUT_DATASET_EXPECTED_DIGEST"] != "sha256:abc" {
		t.Fatalf("expected digest env = %q, want sha256:abc", preparedNode.Env["JUMI_INPUT_DATASET_EXPECTED_DIGEST"])
	}
	if preparedNode.Env["JUMI_INPUT_DATASET_REQUIRES_MATERIALIZATION"] != "true" {
		t.Fatalf("requires materialization env = %q, want true", preparedNode.Env["JUMI_INPUT_DATASET_REQUIRES_MATERIALIZATION"])
	}
	if preparedNode.Env["JUMI_ATTEMPT_ID"] == "" {
		t.Fatal("expected prepared node to include JUMI_ATTEMPT_ID")
	}
	if preparedNode.Placement == nil {
		t.Fatal("expected prepared node placement to be populated")
	}
	if got := preparedNode.Placement.NodeSelector["kubernetes.io/hostname"]; got != "node-a" {
		t.Fatalf("prepared node selector = %q, want node-a", got)
	}
	if got := preparedNode.Env["JUMI_OUTPUT_MANIFEST_PATH"]; got != "" {
		t.Fatalf("prepared manifest path = %q, want empty for node without declared outputs", got)
	}
	if got := engine.Metrics().Render(); !strings.Contains(got, "jumi_input_resolve_requests_total 1") {
		t.Fatalf("expected resolve metric in render: %s", got)
	}
	if len(handoffClient.notifyRequests) == 0 {
		t.Fatal("expected node terminal notification")
	}
	if handoffClient.notifyRequests[0].AttemptID == "" {
		t.Fatal("expected node terminal attemptID to be populated")
	}
	if len(handoffClient.finalizeRequests) != 1 {
		t.Fatalf("finalize sample run calls = %d, want 1", len(handoffClient.finalizeRequests))
	}
	if len(handoffClient.evaluateRequests) != 1 {
		t.Fatalf("evaluate gc calls = %d, want 1", len(handoffClient.evaluateRequests))
	}
	if handoffClient.evaluateRequests[0].SampleRunID != "sample-1" {
		t.Fatalf("evaluate gc sampleRunID = %q, want sample-1", handoffClient.evaluateRequests[0].SampleRunID)
	}
	assertEventTypePresent(t, reg, record.RunID, "node.placement.required_applied")
}

func TestDagEngineInjectsAttemptAwareRuntimeContext(t *testing.T) {
	reg := registry.NewMemoryRegistry()
	adapter := &fakeAdapter{failOn: map[string]bool{}}
	engine := NewDagEngine(reg, adapter)
	specInput := spec.ExecutableRunSpec{
		Run: spec.RunMetadata{RunID: "run-runtime-env", SampleRunID: "sample-runtime-env", SubmittedAt: time.Now().UTC(), FailurePolicy: spec.FailurePolicy{Mode: "fail-fast"}},
		Graph: spec.Graph{
			Nodes: []spec.Node{{
				NodeID:   "produce",
				Image:    "helper-image:latest",
				Command:  []string{"sh", "-c", "echo hi > /out/report"},
				Outputs:  []string{"report"},
				Metadata: map[string]string{"jumi.outputManifestMode": "runtime-helper"},
			}},
		},
	}
	record := spec.RunRecord{RunID: specInput.Run.RunID, Status: spec.RunStatusAccepted, AcceptedAt: time.Now().UTC(), Spec: specInput}
	nodes := []spec.NodeRecord{{RunID: record.RunID, NodeID: "produce", Status: spec.NodeStatusPending}}
	if err := reg.CreateRun(context.Background(), record, nodes); err != nil {
		t.Fatalf("CreateRun() error = %v", err)
	}
	if err := engine.Admit(context.Background(), record); err != nil {
		t.Fatalf("Admit() error = %v", err)
	}
	waitForRunStatus(t, reg, record.RunID, spec.RunStatusSucceeded)
	adapter.mu.Lock()
	preparedNode := adapter.prepared["produce"]
	adapter.mu.Unlock()
	if got := preparedNode.Env["JUMI_RUN_ID"]; got != "run-runtime-env" {
		t.Fatalf("prepared env JUMI_RUN_ID = %q, want run-runtime-env", got)
	}
	if got := preparedNode.Env["JUMI_NODE_ID"]; got != "produce" {
		t.Fatalf("prepared env JUMI_NODE_ID = %q, want produce", got)
	}
	if got := preparedNode.Env["JUMI_ATTEMPT_ID"]; got == "" {
		t.Fatal("prepared env JUMI_ATTEMPT_ID = empty, want attempt context")
	}
	if got := preparedNode.Env["JUMI_OUTPUT_MANIFEST_PATH"]; !strings.Contains(got, "/attempts/") {
		t.Fatalf("prepared env JUMI_OUTPUT_MANIFEST_PATH = %q, want attempt-aware path", got)
	}
}

func TestDagEngineFailsOnConflictingRequiredPlacementIntents(t *testing.T) {
	reg := registry.NewMemoryRegistry()
	adapter := &fakeAdapter{failOn: map[string]bool{}}
	handoffClient := &fakeHandoffClient{
		responses: []handoff.ResolveBindingResponse{
			{
				ResolutionStatus: "RESOLVED",
				Decision:         "remote_fetch",
				PlacementIntent:  handoff.PlacementIntent{Mode: "required_node", NodeName: "node-a"},
				MaterializationPlan: handoff.MaterializationPlan{
					Mode: "remote_fetch",
					URI:  "http://artifact.local/output-a",
				},
			},
			{
				ResolutionStatus: "RESOLVED",
				Decision:         "remote_fetch",
				PlacementIntent:  handoff.PlacementIntent{Mode: "required_node", NodeName: "node-c"},
				MaterializationPlan: handoff.MaterializationPlan{
					Mode: "remote_fetch",
					URI:  "http://artifact.local/output-c",
				},
			},
		},
	}
	engine := NewDagEngineWithHandoff(reg, adapter, handoffClient)
	specInput := spec.ExecutableRunSpec{
		Run: spec.RunMetadata{RunID: "run-placement-conflict", SampleRunID: "sample-placement-conflict", SubmittedAt: time.Now().UTC(), FailurePolicy: spec.FailurePolicy{Mode: "fail-fast"}},
		Graph: spec.Graph{
			Nodes: []spec.Node{
				{NodeID: "a", Image: "busybox:1.36"},
				{NodeID: "c", Image: "busybox:1.36"},
				{NodeID: "b", Image: "busybox:1.36", ArtifactBindings: []spec.ArtifactBinding{
					{BindingName: "left", ChildInputName: "left", ProducerNodeID: "a", ProducerOutputName: "output", ArtifactID: "sample-placement-conflict:a:output", ConsumePolicy: "SameNodeOnly", Required: true},
					{BindingName: "right", ChildInputName: "right", ProducerNodeID: "c", ProducerOutputName: "output", ArtifactID: "sample-placement-conflict:c:output", ConsumePolicy: "SameNodeOnly", Required: true},
				}},
			},
			Edges: [][]string{{"a", "b"}, {"c", "b"}},
		},
	}
	record := spec.RunRecord{RunID: specInput.Run.RunID, Status: spec.RunStatusAccepted, AcceptedAt: time.Now().UTC(), Spec: specInput}
	nodes := []spec.NodeRecord{
		{RunID: record.RunID, NodeID: "a", Status: spec.NodeStatusPending},
		{RunID: record.RunID, NodeID: "c", Status: spec.NodeStatusPending},
		{RunID: record.RunID, NodeID: "b", Status: spec.NodeStatusPending},
	}
	if err := reg.CreateRun(context.Background(), record, nodes); err != nil {
		t.Fatalf("CreateRun() error = %v", err)
	}
	if err := engine.Admit(context.Background(), record); err != nil {
		t.Fatalf("Admit() error = %v", err)
	}
	waitForRunStatus(t, reg, record.RunID, spec.RunStatusFailed)
	runNodes, err := reg.ListNodes(context.Background(), record.RunID)
	if err != nil {
		t.Fatalf("ListNodes() error = %v", err)
	}
	for _, node := range runNodes {
		if node.NodeID != "b" {
			continue
		}
		if node.TerminalFailureReason != "placement_conflict" {
			t.Fatalf("failure reason = %q, want placement_conflict", node.TerminalFailureReason)
		}
	}
}

func TestDagEngineRegistersNodeOutputsOnSuccess(t *testing.T) {
	reg := registry.NewMemoryRegistry()
	adapter := &fakeAdapter{failOn: map[string]bool{}}
	handoffClient := &fakeHandoffClient{}
	engine := NewDagEngineWithHandoff(reg, adapter, handoffClient)
	specInput := spec.ExecutableRunSpec{
		Run: spec.RunMetadata{RunID: "run-outputs", SampleRunID: "sample-out", SubmittedAt: time.Now().UTC(), FailurePolicy: spec.FailurePolicy{Mode: "fail-fast"}},
		Graph: spec.Graph{
			Nodes: []spec.Node{
				{NodeID: "producer", Image: "busybox:1.36", Outputs: []string{"result.json", "logs.txt"}},
			},
		},
	}
	record := spec.RunRecord{RunID: specInput.Run.RunID, Status: spec.RunStatusAccepted, AcceptedAt: time.Now().UTC(), Spec: specInput}
	nodes := []spec.NodeRecord{{RunID: record.RunID, NodeID: "producer", Status: spec.NodeStatusPending}}
	if err := reg.CreateRun(context.Background(), record, nodes); err != nil {
		t.Fatalf("CreateRun() error = %v", err)
	}
	if err := engine.Admit(context.Background(), record); err != nil {
		t.Fatalf("Admit() error = %v", err)
	}
	waitForRunStatus(t, reg, record.RunID, spec.RunStatusSucceeded)
	handoffClient.mu.Lock()
	defer handoffClient.mu.Unlock()
	if len(handoffClient.registerRequests) != 2 {
		t.Fatalf("register artifact calls = %d, want 2", len(handoffClient.registerRequests))
	}
	if handoffClient.registerRequests[0].SampleRunID != "sample-out" {
		t.Fatalf("sampleRunID = %q, want sample-out", handoffClient.registerRequests[0].SampleRunID)
	}
	if handoffClient.registerRequests[0].ProducerNodeID != "producer" {
		t.Fatalf("producerNodeID = %q, want producer", handoffClient.registerRequests[0].ProducerNodeID)
	}
	if handoffClient.registerRequests[0].ProducerAttemptID == "" {
		t.Fatal("expected producerAttemptID to be populated")
	}
	if handoffClient.registerRequests[0].URI == "" && len(handoffClient.registerRequests[0].Locations) == 0 {
		t.Fatal("expected output source location or fetchable URI")
	}
	if handoffClient.registerRequests[0].Digest == "" {
		t.Fatal("expected non-empty digest")
	}
	if handoffClient.registerRequests[0].SizeBytes == 0 {
		t.Fatalf("sizeBytes = %d, want non-zero metadata size", handoffClient.registerRequests[0].SizeBytes)
	}
	if handoffClient.registerRequests[0].NodeName == "" {
		t.Fatal("expected non-empty nodeName")
	}
	if got := engine.Metrics().Render(); !strings.Contains(got, "jumi_artifacts_registered_total 2") {
		t.Fatalf("expected artifact register metric in render: %s", got)
	}
}

func TestDagEngineRegistersOutputMetadataWhenAvailable(t *testing.T) {
	reg := registry.NewMemoryRegistry()
	adapter := &fakeAdapter{
		failOn: map[string]bool{},
		outputs: map[string]map[string]backend.OutputMetadata{
			"producer": {
				"result.json": {
					URI:        "https://artifact-source.local/runs/run-outputs-meta/producer/result.json",
					LogicalURI: "jumi://runs/run-outputs-meta/nodes/producer/outputs/result.json",
					Digest:     "sha256:abc",
					SizeBytes:  4096,
				},
			},
		},
	}
	handoffClient := &fakeHandoffClient{}
	engine := NewDagEngineWithHandoff(reg, adapter, handoffClient)
	specInput := spec.ExecutableRunSpec{
		Run: spec.RunMetadata{RunID: "run-outputs-meta", SampleRunID: "sample-out-meta", SubmittedAt: time.Now().UTC(), FailurePolicy: spec.FailurePolicy{Mode: "fail-fast"}},
		Graph: spec.Graph{
			Nodes: []spec.Node{
				{NodeID: "producer", Image: "busybox:1.36", Outputs: []string{"result.json"}},
			},
		},
	}
	record := spec.RunRecord{RunID: specInput.Run.RunID, Status: spec.RunStatusAccepted, AcceptedAt: time.Now().UTC(), Spec: specInput}
	nodes := []spec.NodeRecord{{RunID: record.RunID, NodeID: "producer", Status: spec.NodeStatusPending}}
	if err := reg.CreateRun(context.Background(), record, nodes); err != nil {
		t.Fatalf("CreateRun() error = %v", err)
	}
	if err := engine.Admit(context.Background(), record); err != nil {
		t.Fatalf("Admit() error = %v", err)
	}
	waitForRunStatus(t, reg, record.RunID, spec.RunStatusSucceeded)
	handoffClient.mu.Lock()
	defer handoffClient.mu.Unlock()
	if len(handoffClient.registerRequests) != 1 {
		t.Fatalf("register artifact calls = %d, want 1", len(handoffClient.registerRequests))
	}
	req := handoffClient.registerRequests[0]
	if req.Digest != "sha256:abc" {
		t.Fatalf("digest = %q, want sha256:abc", req.Digest)
	}
	if req.SizeBytes != 4096 {
		t.Fatalf("sizeBytes = %d, want 4096", req.SizeBytes)
	}
	if req.LogicalURI != "jumi://runs/run-outputs-meta/nodes/producer/outputs/result.json" {
		t.Fatalf("logicalUri = %q, want logical artifact URI", req.LogicalURI)
	}
	if req.URI != "https://artifact-source.local/runs/run-outputs-meta/producer/result.json" {
		t.Fatalf("uri = %q, want fetchable artifact URI", req.URI)
	}
	if len(req.Locations) != 0 {
		t.Fatalf("locations = %#v, want no explicit locations in legacy fake metadata", req.Locations)
	}
}

func TestDagEngineTreatsJumiOutputURIAsLogicalOnly(t *testing.T) {
	reg := registry.NewMemoryRegistry()
	adapter := &fakeAdapter{
		failOn: map[string]bool{},
		outputs: map[string]map[string]backend.OutputMetadata{
			"producer": {
				"result.json": {
					URI:        "jumi://runs/run-outputs-logical/nodes/producer/outputs/result.json",
					LogicalURI: "jumi://runs/run-outputs-logical/nodes/producer/outputs/result.json",
					Digest:     "sha256:abc",
					SizeBytes:  4096,
					NodeName:   "node-a",
					Locations: []provenance.ArtifactLocation{{
						NodeLocal: &provenance.NodeLocalLocation{
							NodeName: "node-a",
							Path:     "/var/lib/jumi-artifacts/cas/sha256/abc",
						},
					}},
				},
			},
		},
	}
	handoffClient := &fakeHandoffClient{}
	engine := NewDagEngineWithHandoff(reg, adapter, handoffClient)
	specInput := spec.ExecutableRunSpec{
		Run:   spec.RunMetadata{RunID: "run-outputs-logical", SampleRunID: "sample-out-logical", SubmittedAt: time.Now().UTC(), FailurePolicy: spec.FailurePolicy{Mode: "fail-fast"}},
		Graph: spec.Graph{Nodes: []spec.Node{{NodeID: "producer", Image: "busybox:1.36", Outputs: []string{"result.json"}}}},
	}
	record := spec.RunRecord{RunID: specInput.Run.RunID, Status: spec.RunStatusAccepted, AcceptedAt: time.Now().UTC(), Spec: specInput}
	nodes := []spec.NodeRecord{{RunID: record.RunID, NodeID: "producer", Status: spec.NodeStatusPending}}
	if err := reg.CreateRun(context.Background(), record, nodes); err != nil {
		t.Fatalf("CreateRun() error = %v", err)
	}
	if err := engine.Admit(context.Background(), record); err != nil {
		t.Fatalf("Admit() error = %v", err)
	}
	waitForRunStatus(t, reg, record.RunID, spec.RunStatusSucceeded)
	handoffClient.mu.Lock()
	defer handoffClient.mu.Unlock()
	if len(handoffClient.registerRequests) != 1 {
		t.Fatalf("register artifact calls = %d, want 1", len(handoffClient.registerRequests))
	}
	req := handoffClient.registerRequests[0]
	if req.URI != "" {
		t.Fatalf("uri = %q, want empty for logical-only jumi URI", req.URI)
	}
	if req.LogicalURI != "jumi://runs/run-outputs-logical/nodes/producer/outputs/result.json" {
		t.Fatalf("logicalUri = %q, want logical artifact URI", req.LogicalURI)
	}
	if len(req.Locations) != 1 || req.Locations[0].NodeLocal == nil {
		t.Fatalf("locations = %#v, want node-local fallback source", req.Locations)
	}
}

func TestDagEngineFailsRunWhenArtifactRegistrationFails(t *testing.T) {
	reg := registry.NewMemoryRegistry()
	adapter := &fakeAdapter{failOn: map[string]bool{}}
	handoffClient := &fakeHandoffClient{registerErr: fmt.Errorf("register down")}
	engine := NewDagEngineWithHandoff(reg, adapter, handoffClient)
	specInput := spec.ExecutableRunSpec{
		Run: spec.RunMetadata{RunID: "run-register-fail", SampleRunID: "sample-register-fail", SubmittedAt: time.Now().UTC(), FailurePolicy: spec.FailurePolicy{Mode: "fail-fast"}},
		Graph: spec.Graph{
			Nodes: []spec.Node{
				{NodeID: "producer", Image: "busybox:1.36", Outputs: []string{"result.json"}},
			},
		},
	}
	record := spec.RunRecord{RunID: specInput.Run.RunID, Status: spec.RunStatusAccepted, AcceptedAt: time.Now().UTC(), Spec: specInput}
	nodes := []spec.NodeRecord{{RunID: record.RunID, NodeID: "producer", Status: spec.NodeStatusPending}}
	if err := reg.CreateRun(context.Background(), record, nodes); err != nil {
		t.Fatalf("CreateRun() error = %v", err)
	}
	if err := engine.Admit(context.Background(), record); err != nil {
		t.Fatalf("Admit() error = %v", err)
	}
	waitForRunStatus(t, reg, record.RunID, spec.RunStatusFailed)

	run, err := reg.GetRun(context.Background(), record.RunID)
	if err != nil {
		t.Fatalf("GetRun() error = %v", err)
	}
	if run.TerminalFailureReason != "register_artifact_error" {
		t.Fatalf("TerminalFailureReason = %q, want register_artifact_error", run.TerminalFailureReason)
	}
	if got := engine.Metrics().Render(); strings.Contains(got, "jumi_artifacts_registered_total 1") {
		t.Fatalf("unexpected artifact register metric in render: %s", got)
	}
}

func TestDagEngineFailsSuccessfulRunWhenFinalizeFails(t *testing.T) {
	reg := registry.NewMemoryRegistry()
	adapter := &fakeAdapter{failOn: map[string]bool{}}
	handoffClient := &fakeHandoffClient{finalizeErr: fmt.Errorf("finalize down")}
	engine := NewDagEngineWithHandoff(reg, adapter, handoffClient)
	specInput := spec.ExecutableRunSpec{
		Run: spec.RunMetadata{RunID: "run-finalize-fail", SampleRunID: "sample-finalize-fail", SubmittedAt: time.Now().UTC(), FailurePolicy: spec.FailurePolicy{Mode: "fail-fast"}},
		Graph: spec.Graph{
			Nodes: []spec.Node{
				{NodeID: "producer", Image: "busybox:1.36"},
			},
		},
	}
	record := spec.RunRecord{RunID: specInput.Run.RunID, Status: spec.RunStatusAccepted, AcceptedAt: time.Now().UTC(), Spec: specInput}
	nodes := []spec.NodeRecord{{RunID: record.RunID, NodeID: "producer", Status: spec.NodeStatusPending}}
	if err := reg.CreateRun(context.Background(), record, nodes); err != nil {
		t.Fatalf("CreateRun() error = %v", err)
	}
	if err := engine.Admit(context.Background(), record); err != nil {
		t.Fatalf("Admit() error = %v", err)
	}
	waitForRunStatus(t, reg, record.RunID, spec.RunStatusFailed)

	run, err := reg.GetRun(context.Background(), record.RunID)
	if err != nil {
		t.Fatalf("GetRun() error = %v", err)
	}
	if run.TerminalFailureReason != "handoff_finalize_error" {
		t.Fatalf("TerminalFailureReason = %q, want handoff_finalize_error", run.TerminalFailureReason)
	}
	if got := engine.Metrics().Render(); strings.Contains(got, "jumi_sample_runs_finalized_total 1") {
		t.Fatalf("unexpected finalize metric in render: %s", got)
	}
}

func TestDagEngineFailsSuccessfulRunWhenEvaluateGCFails(t *testing.T) {
	reg := registry.NewMemoryRegistry()
	adapter := &fakeAdapter{failOn: map[string]bool{}}
	handoffClient := &fakeHandoffClient{evaluateErr: fmt.Errorf("gc down")}
	engine := NewDagEngineWithHandoff(reg, adapter, handoffClient)
	specInput := spec.ExecutableRunSpec{
		Run: spec.RunMetadata{RunID: "run-gc-fail", SampleRunID: "sample-gc-fail", SubmittedAt: time.Now().UTC(), FailurePolicy: spec.FailurePolicy{Mode: "fail-fast"}},
		Graph: spec.Graph{
			Nodes: []spec.Node{
				{NodeID: "producer", Image: "busybox:1.36"},
			},
		},
	}
	record := spec.RunRecord{RunID: specInput.Run.RunID, Status: spec.RunStatusAccepted, AcceptedAt: time.Now().UTC(), Spec: specInput}
	nodes := []spec.NodeRecord{{RunID: record.RunID, NodeID: "producer", Status: spec.NodeStatusPending}}
	if err := reg.CreateRun(context.Background(), record, nodes); err != nil {
		t.Fatalf("CreateRun() error = %v", err)
	}
	if err := engine.Admit(context.Background(), record); err != nil {
		t.Fatalf("Admit() error = %v", err)
	}
	waitForRunStatus(t, reg, record.RunID, spec.RunStatusFailed)

	run, err := reg.GetRun(context.Background(), record.RunID)
	if err != nil {
		t.Fatalf("GetRun() error = %v", err)
	}
	if run.TerminalFailureReason != "handoff_gc_evaluate_error" {
		t.Fatalf("TerminalFailureReason = %q, want handoff_gc_evaluate_error", run.TerminalFailureReason)
	}
	if got := engine.Metrics().Render(); strings.Contains(got, "jumi_gc_evaluate_requests_total 1") {
		t.Fatalf("unexpected gc evaluate metric in render: %s", got)
	}
}

func TestDagEngineFailsNodeWhenResolveBindingErrors(t *testing.T) {
	reg := registry.NewMemoryRegistry()
	adapter := &fakeAdapter{failOn: map[string]bool{}}
	handoffClient := &fakeHandoffClient{resolveErr: fmt.Errorf("handoff unavailable")}
	engine := NewDagEngineWithHandoff(reg, adapter, handoffClient)
	specInput := spec.ExecutableRunSpec{
		Run: spec.RunMetadata{RunID: "run-resolve-error", SampleRunID: "sample-resolve-error", SubmittedAt: time.Now().UTC(), FailurePolicy: spec.FailurePolicy{Mode: "fail-fast"}},
		Graph: spec.Graph{
			Nodes: []spec.Node{
				{NodeID: "a", Image: "busybox:1.36", Outputs: []string{"output"}},
				{NodeID: "b", Image: "busybox:1.36", ArtifactBindings: []spec.ArtifactBinding{{
					BindingName:        "dataset",
					ChildInputName:     "dataset",
					ProducerNodeID:     "a",
					ProducerOutputName: "output",
					ConsumePolicy:      "RemoteOK",
					Required:           true,
				}}},
			},
			Edges: [][]string{{"a", "b"}},
		},
	}
	record := spec.RunRecord{RunID: specInput.Run.RunID, Status: spec.RunStatusAccepted, AcceptedAt: time.Now().UTC(), Spec: specInput}
	nodes := []spec.NodeRecord{{RunID: record.RunID, NodeID: "a", Status: spec.NodeStatusPending}, {RunID: record.RunID, NodeID: "b", Status: spec.NodeStatusPending}}
	if err := reg.CreateRun(context.Background(), record, nodes); err != nil {
		t.Fatalf("CreateRun() error = %v", err)
	}
	if err := engine.Admit(context.Background(), record); err != nil {
		t.Fatalf("Admit() error = %v", err)
	}
	waitForRunStatus(t, reg, record.RunID, spec.RunStatusFailed)

	runNodes, err := reg.ListNodes(context.Background(), record.RunID)
	if err != nil {
		t.Fatalf("ListNodes() error = %v", err)
	}
	statuses := map[string]spec.NodeStatus{}
	reasons := map[string]string{}
	for _, node := range runNodes {
		statuses[node.NodeID] = node.Status
		reasons[node.NodeID] = node.TerminalFailureReason
	}
	if statuses["b"] != spec.NodeStatusFailed {
		t.Fatalf("node b status = %q, want Failed", statuses["b"])
	}
	if reasons["b"] != "resolve_handoff_error" {
		t.Fatalf("node b failureReason = %q, want resolve_handoff_error", reasons["b"])
	}
}

func TestDagEngineFailsNodeWhenRequiredBindingMissing(t *testing.T) {
	reg := registry.NewMemoryRegistry()
	adapter := &fakeAdapter{failOn: map[string]bool{}}
	handoffClient := &fakeHandoffClient{
		response: handoff.ResolveBindingResponse{ResolutionStatus: "MISSING", Decision: "unavailable"},
	}
	engine := NewDagEngineWithHandoff(reg, adapter, handoffClient)
	specInput := spec.ExecutableRunSpec{
		Run: spec.RunMetadata{RunID: "run-binding-missing", SampleRunID: "sample-binding-missing", SubmittedAt: time.Now().UTC(), FailurePolicy: spec.FailurePolicy{Mode: "fail-fast"}},
		Graph: spec.Graph{
			Nodes: []spec.Node{
				{NodeID: "a", Image: "busybox:1.36", Outputs: []string{"output"}},
				{NodeID: "b", Image: "busybox:1.36", ArtifactBindings: []spec.ArtifactBinding{{
					BindingName:        "dataset",
					ChildInputName:     "dataset",
					ProducerNodeID:     "a",
					ProducerOutputName: "output",
					ConsumePolicy:      "RemoteOK",
					Required:           true,
				}}},
			},
			Edges: [][]string{{"a", "b"}},
		},
	}
	record := spec.RunRecord{RunID: specInput.Run.RunID, Status: spec.RunStatusAccepted, AcceptedAt: time.Now().UTC(), Spec: specInput}
	nodes := []spec.NodeRecord{{RunID: record.RunID, NodeID: "a", Status: spec.NodeStatusPending}, {RunID: record.RunID, NodeID: "b", Status: spec.NodeStatusPending}}
	if err := reg.CreateRun(context.Background(), record, nodes); err != nil {
		t.Fatalf("CreateRun() error = %v", err)
	}
	if err := engine.Admit(context.Background(), record); err != nil {
		t.Fatalf("Admit() error = %v", err)
	}
	waitForRunStatus(t, reg, record.RunID, spec.RunStatusFailed)

	runNodes, err := reg.ListNodes(context.Background(), record.RunID)
	if err != nil {
		t.Fatalf("ListNodes() error = %v", err)
	}
	for _, node := range runNodes {
		if node.NodeID == "b" && node.TerminalFailureReason != "input_resolution_missing" {
			t.Fatalf("node b failureReason = %q, want input_resolution_missing", node.TerminalFailureReason)
		}
	}
}

func TestDagEngineFailsNodeWhenProducerFailedBindingIsMissing(t *testing.T) {
	reg := registry.NewMemoryRegistry()
	adapter := &fakeAdapter{failOn: map[string]bool{}}
	handoffClient := &fakeHandoffClient{
		response: handoff.ResolveBindingResponse{ResolutionStatus: "MISSING", Decision: "producer_failed"},
	}
	engine := NewDagEngineWithHandoff(reg, adapter, handoffClient)
	specInput := spec.ExecutableRunSpec{
		Run: spec.RunMetadata{RunID: "run-binding-producer-failed", SampleRunID: "sample-binding-producer-failed", SubmittedAt: time.Now().UTC(), FailurePolicy: spec.FailurePolicy{Mode: "fail-fast"}},
		Graph: spec.Graph{
			Nodes: []spec.Node{
				{NodeID: "a", Image: "busybox:1.36", Outputs: []string{"output"}},
				{NodeID: "b", Image: "busybox:1.36", ArtifactBindings: []spec.ArtifactBinding{{
					BindingName:        "dataset",
					ChildInputName:     "dataset",
					ProducerNodeID:     "a",
					ProducerOutputName: "output",
					ConsumePolicy:      "RemoteOK",
					Required:           true,
				}}},
			},
			Edges: [][]string{{"a", "b"}},
		},
	}
	record := spec.RunRecord{RunID: specInput.Run.RunID, Status: spec.RunStatusAccepted, AcceptedAt: time.Now().UTC(), Spec: specInput}
	nodes := []spec.NodeRecord{{RunID: record.RunID, NodeID: "a", Status: spec.NodeStatusPending}, {RunID: record.RunID, NodeID: "b", Status: spec.NodeStatusPending}}
	if err := reg.CreateRun(context.Background(), record, nodes); err != nil {
		t.Fatalf("CreateRun() error = %v", err)
	}
	if err := engine.Admit(context.Background(), record); err != nil {
		t.Fatalf("Admit() error = %v", err)
	}
	waitForRunStatus(t, reg, record.RunID, spec.RunStatusFailed)

	runNodes, err := reg.ListNodes(context.Background(), record.RunID)
	if err != nil {
		t.Fatalf("ListNodes() error = %v", err)
	}
	for _, node := range runNodes {
		if node.NodeID == "b" && node.TerminalFailureReason != "input_resolution_producer_failed" {
			t.Fatalf("node b failureReason = %q, want input_resolution_producer_failed", node.TerminalFailureReason)
		}
	}
}

func TestDagEngineFailsSuccessfulNodeWhenNotifyNodeTerminalFails(t *testing.T) {
	reg := registry.NewMemoryRegistry()
	adapter := &fakeAdapter{failOn: map[string]bool{}}
	handoffClient := &fakeHandoffClient{notifyErr: fmt.Errorf("notify down")}
	engine := NewDagEngineWithHandoff(reg, adapter, handoffClient)
	specInput := spec.ExecutableRunSpec{
		Run:   spec.RunMetadata{RunID: "run-notify-success-fail", SampleRunID: "sample-notify-success-fail", SubmittedAt: time.Now().UTC(), FailurePolicy: spec.FailurePolicy{Mode: "fail-fast"}},
		Graph: spec.Graph{Nodes: []spec.Node{{NodeID: "a", Image: "busybox:1.36"}}},
	}
	record := spec.RunRecord{RunID: specInput.Run.RunID, Status: spec.RunStatusAccepted, AcceptedAt: time.Now().UTC(), Spec: specInput}
	nodes := []spec.NodeRecord{{RunID: record.RunID, NodeID: "a", Status: spec.NodeStatusPending}}
	if err := reg.CreateRun(context.Background(), record, nodes); err != nil {
		t.Fatalf("CreateRun() error = %v", err)
	}
	if err := engine.Admit(context.Background(), record); err != nil {
		t.Fatalf("Admit() error = %v", err)
	}
	waitForRunStatus(t, reg, record.RunID, spec.RunStatusFailed)

	run, err := reg.GetRun(context.Background(), record.RunID)
	if err != nil {
		t.Fatalf("GetRun() error = %v", err)
	}
	if run.TerminalFailureReason != "notify_node_terminal_error" {
		t.Fatalf("run failureReason = %q, want notify_node_terminal_error", run.TerminalFailureReason)
	}
	runNodes, err := reg.ListNodes(context.Background(), record.RunID)
	if err != nil {
		t.Fatalf("ListNodes() error = %v", err)
	}
	for _, node := range runNodes {
		if node.NodeID == "a" && node.TerminalFailureReason != "notify_node_terminal_error" {
			t.Fatalf("node a failureReason = %q, want notify_node_terminal_error", node.TerminalFailureReason)
		}
	}
}

func TestDagEngineRetriesResolveBindingBeforeSuccess(t *testing.T) {
	reg := registry.NewMemoryRegistry()
	adapter := &fakeAdapter{failOn: map[string]bool{}}
	handoffClient := &fakeHandoffClient{
		resolveErrs: []error{status.Error(codes.Unavailable, "temporary handoff error"), nil},
		response: handoff.ResolveBindingResponse{
			ResolutionStatus: "RESOLVED",
			Decision:         "remote_fetch",
			PlacementIntent:  handoff.PlacementIntent{Mode: "required_node", NodeName: "node-a"},
			MaterializationPlan: handoff.MaterializationPlan{
				Mode: "remote_fetch",
				URI:  "http://artifact.local/output",
			},
		},
	}
	engine := NewDagEngineWithHandoff(reg, adapter, handoffClient)
	specInput := spec.ExecutableRunSpec{
		Run: spec.RunMetadata{RunID: "run-resolve-retry-success", SampleRunID: "sample-resolve-retry-success", SubmittedAt: time.Now().UTC(), FailurePolicy: spec.FailurePolicy{Mode: "fail-fast"}},
		Graph: spec.Graph{
			Nodes: []spec.Node{
				{NodeID: "a", Image: "busybox:1.36", Outputs: []string{"output"}},
				{NodeID: "b", Image: "busybox:1.36", ArtifactBindings: []spec.ArtifactBinding{{
					BindingName:        "dataset",
					ChildInputName:     "dataset",
					ProducerNodeID:     "a",
					ProducerOutputName: "output",
					ConsumePolicy:      "RemoteOK",
					Required:           true,
				}}},
			},
			Edges: [][]string{{"a", "b"}},
		},
	}
	record := spec.RunRecord{RunID: specInput.Run.RunID, Status: spec.RunStatusAccepted, AcceptedAt: time.Now().UTC(), Spec: specInput}
	nodes := []spec.NodeRecord{{RunID: record.RunID, NodeID: "a", Status: spec.NodeStatusPending}, {RunID: record.RunID, NodeID: "b", Status: spec.NodeStatusPending}}
	if err := reg.CreateRun(context.Background(), record, nodes); err != nil {
		t.Fatalf("CreateRun() error = %v", err)
	}
	if err := engine.Admit(context.Background(), record); err != nil {
		t.Fatalf("Admit() error = %v", err)
	}
	waitForRunStatus(t, reg, record.RunID, spec.RunStatusSucceeded)
	handoffClient.mu.Lock()
	defer handoffClient.mu.Unlock()
	if len(handoffClient.requests) != 2 {
		t.Fatalf("resolve binding calls = %d, want 2", len(handoffClient.requests))
	}
}

func TestDagEngineFailsAfterResolveBindingRetryBudgetExhausted(t *testing.T) {
	reg := registry.NewMemoryRegistry()
	adapter := &fakeAdapter{failOn: map[string]bool{}}
	handoffClient := &fakeHandoffClient{
		resolveErrs: []error{
			status.Error(codes.Unavailable, "temporary handoff error"),
			status.Error(codes.Unavailable, "temporary handoff error"),
			status.Error(codes.Unavailable, "temporary handoff error"),
		},
	}
	engine := NewDagEngineWithHandoff(reg, adapter, handoffClient)
	specInput := spec.ExecutableRunSpec{
		Run: spec.RunMetadata{RunID: "run-resolve-retry-fail", SampleRunID: "sample-resolve-retry-fail", SubmittedAt: time.Now().UTC(), FailurePolicy: spec.FailurePolicy{Mode: "fail-fast"}},
		Graph: spec.Graph{
			Nodes: []spec.Node{
				{NodeID: "a", Image: "busybox:1.36", Outputs: []string{"output"}},
				{NodeID: "b", Image: "busybox:1.36", ArtifactBindings: []spec.ArtifactBinding{{
					BindingName:        "dataset",
					ChildInputName:     "dataset",
					ProducerNodeID:     "a",
					ProducerOutputName: "output",
					ConsumePolicy:      "RemoteOK",
					Required:           true,
				}}},
			},
			Edges: [][]string{{"a", "b"}},
		},
	}
	record := spec.RunRecord{RunID: specInput.Run.RunID, Status: spec.RunStatusAccepted, AcceptedAt: time.Now().UTC(), Spec: specInput}
	nodes := []spec.NodeRecord{{RunID: record.RunID, NodeID: "a", Status: spec.NodeStatusPending}, {RunID: record.RunID, NodeID: "b", Status: spec.NodeStatusPending}}
	if err := reg.CreateRun(context.Background(), record, nodes); err != nil {
		t.Fatalf("CreateRun() error = %v", err)
	}
	if err := engine.Admit(context.Background(), record); err != nil {
		t.Fatalf("Admit() error = %v", err)
	}
	waitForRunStatus(t, reg, record.RunID, spec.RunStatusFailed)
	handoffClient.mu.Lock()
	defer handoffClient.mu.Unlock()
	if len(handoffClient.requests) != resolveBindingMaxAttempts {
		t.Fatalf("resolve binding calls = %d, want %d", len(handoffClient.requests), resolveBindingMaxAttempts)
	}
}

func TestDagEngineDoesNotRetryResolveBindingForNonTransientError(t *testing.T) {
	reg := registry.NewMemoryRegistry()
	adapter := &fakeAdapter{failOn: map[string]bool{}}
	handoffClient := &fakeHandoffClient{
		resolveErr: status.Error(codes.NotFound, "sample run lifecycle not found"),
	}
	engine := NewDagEngineWithHandoff(reg, adapter, handoffClient)
	specInput := spec.ExecutableRunSpec{
		Run: spec.RunMetadata{RunID: "run-resolve-no-retry", SampleRunID: "sample-resolve-no-retry", SubmittedAt: time.Now().UTC(), FailurePolicy: spec.FailurePolicy{Mode: "fail-fast"}},
		Graph: spec.Graph{
			Nodes: []spec.Node{
				{NodeID: "a", Image: "busybox:1.36", Outputs: []string{"output"}},
				{NodeID: "b", Image: "busybox:1.36", ArtifactBindings: []spec.ArtifactBinding{{
					BindingName:        "dataset",
					ChildInputName:     "dataset",
					ProducerNodeID:     "a",
					ProducerOutputName: "output",
					ConsumePolicy:      "RemoteOK",
					Required:           true,
				}}},
			},
			Edges: [][]string{{"a", "b"}},
		},
	}
	record := spec.RunRecord{RunID: specInput.Run.RunID, Status: spec.RunStatusAccepted, AcceptedAt: time.Now().UTC(), Spec: specInput}
	nodes := []spec.NodeRecord{{RunID: record.RunID, NodeID: "a", Status: spec.NodeStatusPending}, {RunID: record.RunID, NodeID: "b", Status: spec.NodeStatusPending}}
	if err := reg.CreateRun(context.Background(), record, nodes); err != nil {
		t.Fatalf("CreateRun() error = %v", err)
	}
	if err := engine.Admit(context.Background(), record); err != nil {
		t.Fatalf("Admit() error = %v", err)
	}
	waitForRunStatus(t, reg, record.RunID, spec.RunStatusFailed)
	handoffClient.mu.Lock()
	defer handoffClient.mu.Unlock()
	if len(handoffClient.requests) != 1 {
		t.Fatalf("resolve binding calls = %d, want 1", len(handoffClient.requests))
	}
	assertEventAbsent(t, reg, record.RunID, "node.input_resolve_retry")
}

func TestDagEngineRetriesResolveBindingForHTTP503(t *testing.T) {
	reg := registry.NewMemoryRegistry()
	adapter := &fakeAdapter{failOn: map[string]bool{}}
	handoffClient := &fakeHandoffClient{
		resolveErrs: []error{&handoff.HTTPError{StatusCode: 503, Op: "resolve"}, nil},
		response: handoff.ResolveBindingResponse{
			ResolutionStatus: "RESOLVED",
			Decision:         "remote_fetch",
			PlacementIntent:  handoff.PlacementIntent{Mode: "required_node", NodeName: "node-a"},
			MaterializationPlan: handoff.MaterializationPlan{
				Mode: "remote_fetch",
				URI:  "http://artifact.local/output",
			},
		},
	}
	engine := NewDagEngineWithHandoff(reg, adapter, handoffClient)
	specInput := spec.ExecutableRunSpec{
		Run: spec.RunMetadata{RunID: "run-resolve-http503-retry", SampleRunID: "sample-resolve-http503-retry", SubmittedAt: time.Now().UTC(), FailurePolicy: spec.FailurePolicy{Mode: "fail-fast"}},
		Graph: spec.Graph{
			Nodes: []spec.Node{
				{NodeID: "a", Image: "busybox:1.36", Outputs: []string{"output"}},
				{NodeID: "b", Image: "busybox:1.36", ArtifactBindings: []spec.ArtifactBinding{{
					BindingName:        "dataset",
					ChildInputName:     "dataset",
					ProducerNodeID:     "a",
					ProducerOutputName: "output",
					ConsumePolicy:      "RemoteOK",
					Required:           true,
				}}},
			},
			Edges: [][]string{{"a", "b"}},
		},
	}
	record := spec.RunRecord{RunID: specInput.Run.RunID, Status: spec.RunStatusAccepted, AcceptedAt: time.Now().UTC(), Spec: specInput}
	nodes := []spec.NodeRecord{{RunID: record.RunID, NodeID: "a", Status: spec.NodeStatusPending}, {RunID: record.RunID, NodeID: "b", Status: spec.NodeStatusPending}}
	if err := reg.CreateRun(context.Background(), record, nodes); err != nil {
		t.Fatalf("CreateRun() error = %v", err)
	}
	if err := engine.Admit(context.Background(), record); err != nil {
		t.Fatalf("Admit() error = %v", err)
	}
	waitForRunStatus(t, reg, record.RunID, spec.RunStatusSucceeded)
	handoffClient.mu.Lock()
	defer handoffClient.mu.Unlock()
	if len(handoffClient.requests) != 2 {
		t.Fatalf("resolve binding calls = %d, want 2", len(handoffClient.requests))
	}
	assertEventPresent(t, reg, record.RunID, "node.input_resolve_retry", "resolve_handoff_error")
}

func TestDagEngineDoesNotMarkAttemptCompletedBeforeArtifactRegistrationOrNotify(t *testing.T) {
	reg := registry.NewMemoryRegistry()
	adapter := &fakeAdapter{failOn: map[string]bool{}}
	handoffClient := &fakeHandoffClient{}
	engine := NewDagEngineWithHandoff(reg, adapter, handoffClient)
	specInput := spec.ExecutableRunSpec{
		Run:   spec.RunMetadata{RunID: "run-attempt-order", SampleRunID: "sample-attempt-order", SubmittedAt: time.Now().UTC(), FailurePolicy: spec.FailurePolicy{Mode: "fail-fast"}},
		Graph: spec.Graph{Nodes: []spec.Node{{NodeID: "a", Image: "busybox:1.36", Outputs: []string{"output"}}}},
	}
	record := spec.RunRecord{RunID: specInput.Run.RunID, Status: spec.RunStatusAccepted, AcceptedAt: time.Now().UTC(), Spec: specInput}
	nodes := []spec.NodeRecord{{RunID: record.RunID, NodeID: "a", Status: spec.NodeStatusPending}}
	if err := reg.CreateRun(context.Background(), record, nodes); err != nil {
		t.Fatalf("CreateRun() error = %v", err)
	}

	checkNotCompleted := func(where string) {
		attempts, err := reg.ListAttempts(context.Background(), record.RunID, "a")
		if err != nil {
			t.Fatalf("%s: ListAttempts() error = %v", where, err)
		}
		if len(attempts) != 1 {
			t.Fatalf("%s: attempts = %d, want 1", where, len(attempts))
		}
		if attempts[0].Status == spec.AttemptStatusCompleted {
			t.Fatalf("%s: attempt status = Completed before final success bookkeeping", where)
		}
	}

	handoffClient.registerHook = func(_ handoff.RegisterArtifactRequest) { checkNotCompleted("register") }
	handoffClient.notifyHook = func(_ handoff.NotifyNodeTerminalRequest) { checkNotCompleted("notify") }

	if err := engine.Admit(context.Background(), record); err != nil {
		t.Fatalf("Admit() error = %v", err)
	}
	waitForRunStatus(t, reg, record.RunID, spec.RunStatusSucceeded)

	attempts, err := reg.ListAttempts(context.Background(), record.RunID, "a")
	if err != nil {
		t.Fatalf("ListAttempts() error = %v", err)
	}
	if len(attempts) != 1 {
		t.Fatalf("attempts = %d, want 1", len(attempts))
	}
	if attempts[0].Status != spec.AttemptStatusCompleted {
		t.Fatalf("final attempt status = %q, want Completed", attempts[0].Status)
	}
}

func TestDagEnginePendingResolveBindingUsesSeparateRetryBudget(t *testing.T) {
	reg := registry.NewMemoryRegistry()
	adapter := &fakeAdapter{failOn: map[string]bool{}}
	handoffClient := &fakeHandoffClient{
		responses: []handoff.ResolveBindingResponse{
			{ResolutionStatus: "PENDING", Retryable: true},
			{ResolutionStatus: "PENDING", Retryable: true},
			{
				ResolutionStatus: "RESOLVED",
				Decision:         "remote_fetch",
				PlacementIntent:  handoff.PlacementIntent{Mode: "required_node", NodeName: "node-a"},
				MaterializationPlan: handoff.MaterializationPlan{
					Mode: "remote_fetch",
					URI:  "http://artifact.local/output",
				},
			},
		},
	}
	engine := NewDagEngineWithHandoff(reg, adapter, handoffClient)
	specInput := spec.ExecutableRunSpec{
		Run: spec.RunMetadata{RunID: "run-resolve-pending-then-ready", SampleRunID: "sample-resolve-pending-then-ready", SubmittedAt: time.Now().UTC(), FailurePolicy: spec.FailurePolicy{Mode: "fail-fast"}},
		Graph: spec.Graph{
			Nodes: []spec.Node{
				{NodeID: "a", Image: "busybox:1.36", Outputs: []string{"output"}},
				{NodeID: "b", Image: "busybox:1.36", ArtifactBindings: []spec.ArtifactBinding{{
					BindingName:        "dataset",
					ChildInputName:     "dataset",
					ProducerNodeID:     "a",
					ProducerOutputName: "output",
					ConsumePolicy:      "RemoteOK",
					Required:           true,
				}}},
			},
			Edges: [][]string{{"a", "b"}},
		},
	}
	record := spec.RunRecord{RunID: specInput.Run.RunID, Status: spec.RunStatusAccepted, AcceptedAt: time.Now().UTC(), Spec: specInput}
	nodes := []spec.NodeRecord{{RunID: record.RunID, NodeID: "a", Status: spec.NodeStatusPending}, {RunID: record.RunID, NodeID: "b", Status: spec.NodeStatusPending}}
	if err := reg.CreateRun(context.Background(), record, nodes); err != nil {
		t.Fatalf("CreateRun() error = %v", err)
	}
	if err := engine.Admit(context.Background(), record); err != nil {
		t.Fatalf("Admit() error = %v", err)
	}
	waitForRunStatusWithin(t, reg, record.RunID, spec.RunStatusSucceeded, 6*time.Second)

	handoffClient.mu.Lock()
	defer handoffClient.mu.Unlock()
	if len(handoffClient.requests) != 3 {
		t.Fatalf("resolve binding calls = %d, want 3", len(handoffClient.requests))
	}
	assertEventTypePresent(t, reg, record.RunID, "node.input_resolve_pending")
}

func (f *fakeAdapter) StartNode(ctx context.Context, prepared backend.PreparedNode) (backend.Handle, error) {
	if f.startDelay > 0 {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(f.startDelay):
		}
	}
	p := prepared.(fakePrepared)
	f.mu.Lock()
	f.order = append(f.order, p.nodeID)
	f.mu.Unlock()
	return fakeHandle(p), nil
}

func (f *fakeAdapter) ObserveNode(_ context.Context, _ backend.Handle) (*backend.OptionalKueueInfo, error) {
	return f.observe, nil
}

func (f *fakeAdapter) WaitNode(ctx context.Context, handle backend.Handle) (backend.ExecutionResult, error) {
	h := handle.(fakeHandle)
	if ch := f.channelFor(h.nodeID); ch != nil {
		select {
		case <-ctx.Done():
			return backend.ExecutionResult{TerminalStopCause: "canceled", TerminalFailureReason: "cancellation_requested"}, ctx.Err()
		case <-ch:
		}
	}
	if f.failOn[h.nodeID] {
		return backend.ExecutionResult{TerminalStopCause: "failed", TerminalFailureReason: "backend_wait_error"}, fmt.Errorf("forced failure")
	}
	return backend.ExecutionResult{Succeeded: true, TerminalStopCause: "finished"}, nil
}

func (f *fakeAdapter) CancelNode(_ context.Context, handle backend.Handle) error {
	h := handle.(fakeHandle)
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.canceled == nil {
		f.canceled = map[string]bool{}
	}
	f.canceled[h.nodeID] = true
	if ch, ok := f.waitCh[h.nodeID]; ok {
		select {
		case <-ch:
		default:
			close(ch)
		}
	}
	return nil
}

func (f *fakeAdapter) CollectOutputMetadata(_ context.Context, handle backend.Handle, node spec.Node) (map[string]backend.OutputMetadata, error) {
	h := handle.(fakeHandle)
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.forceMetadataUnavailable {
		return nil, backend.ErrOutputMetadataUnavailable
	}
	if f.outputs == nil {
		if len(node.Outputs) == 0 {
			return nil, backend.ErrOutputMetadataUnavailable
		}
		out := make(map[string]backend.OutputMetadata, len(node.Outputs))
		for _, outputName := range node.Outputs {
			out[outputName] = backend.OutputMetadata{
				OutputName:        outputName,
				LogicalURI:        "jumi://runs/test-run/nodes/" + h.nodeID + "/outputs/" + outputName,
				Digest:            "sha256:test",
				SizeBytes:         1,
				ProducerAttemptID: "attempt-1",
				NodeName:          "node-a",
				PodName:           h.nodeID + "-pod",
				Locations: []provenance.ArtifactLocation{{
					NodeLocal: &provenance.NodeLocalLocation{
						NodeName: "node-a",
						Path:     "/var/lib/jumi-artifacts/cas/sha256/test",
					},
				}},
			}
		}
		return out, nil
	}
	metadata, ok := f.outputs[h.nodeID]
	if !ok {
		return nil, backend.ErrOutputMetadataUnavailable
	}
	out := make(map[string]backend.OutputMetadata, len(metadata))
	for k, v := range metadata {
		out[k] = v
	}
	return out, nil
}

func (f *fakeAdapter) channelFor(nodeID string) chan struct{} {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.waitCh == nil {
		return nil
	}
	return f.waitCh[nodeID]
}

func TestDagEngineExecutesLinearGraph(t *testing.T) {
	reg := registry.NewMemoryRegistry()
	adapter := &fakeAdapter{failOn: map[string]bool{}}
	engine := NewDagEngine(reg, adapter)
	specInput := spec.ExecutableRunSpec{
		Run: spec.RunMetadata{RunID: "run-linear", SubmittedAt: time.Now().UTC(), FailurePolicy: spec.FailurePolicy{Mode: "fail-fast"}},
		Graph: spec.Graph{
			Nodes: []spec.Node{{NodeID: "a", Image: "busybox:1.36"}, {NodeID: "b", Image: "busybox:1.36"}},
			Edges: [][]string{{"a", "b"}},
		},
	}
	record := spec.RunRecord{RunID: specInput.Run.RunID, Status: spec.RunStatusAccepted, AcceptedAt: time.Now().UTC(), Spec: specInput}
	nodes := []spec.NodeRecord{{RunID: record.RunID, NodeID: "a", Status: spec.NodeStatusPending}, {RunID: record.RunID, NodeID: "b", Status: spec.NodeStatusPending}}
	if err := reg.CreateRun(context.Background(), record, nodes); err != nil {
		t.Fatalf("CreateRun() error = %v", err)
	}
	if err := engine.Admit(context.Background(), record); err != nil {
		t.Fatalf("Admit() error = %v", err)
	}
	waitForRunStatus(t, reg, record.RunID, spec.RunStatusSucceeded)
	adapter.mu.Lock()
	defer adapter.mu.Unlock()
	if len(adapter.order) != 2 || adapter.order[0] != "a" || adapter.order[1] != "b" {
		t.Fatalf("unexpected execution order: %v", adapter.order)
	}
	if !adapter.canceled[""] && len(adapter.canceled) != 0 {
		t.Fatalf("unexpected cancellations: %v", adapter.canceled)
	}
}

func TestDagEngineSkipsDownstreamOnFailure(t *testing.T) {
	reg := registry.NewMemoryRegistry()
	adapter := &fakeAdapter{failOn: map[string]bool{"a": true}}
	engine := NewDagEngine(reg, adapter)
	specInput := spec.ExecutableRunSpec{
		Run: spec.RunMetadata{RunID: "run-fail", SubmittedAt: time.Now().UTC(), FailurePolicy: spec.FailurePolicy{Mode: "fail-fast"}},
		Graph: spec.Graph{
			Nodes: []spec.Node{{NodeID: "a", Image: "busybox:1.36"}, {NodeID: "b", Image: "busybox:1.36"}},
			Edges: [][]string{{"a", "b"}},
		},
	}
	record := spec.RunRecord{RunID: specInput.Run.RunID, Status: spec.RunStatusAccepted, AcceptedAt: time.Now().UTC(), Spec: specInput}
	nodes := []spec.NodeRecord{{RunID: record.RunID, NodeID: "a", Status: spec.NodeStatusPending}, {RunID: record.RunID, NodeID: "b", Status: spec.NodeStatusPending}}
	if err := reg.CreateRun(context.Background(), record, nodes); err != nil {
		t.Fatalf("CreateRun() error = %v", err)
	}
	if err := engine.Admit(context.Background(), record); err != nil {
		t.Fatalf("Admit() error = %v", err)
	}
	waitForRunStatus(t, reg, record.RunID, spec.RunStatusFailed)
	runNodes, err := reg.ListNodes(context.Background(), record.RunID)
	if err != nil {
		t.Fatalf("ListNodes() error = %v", err)
	}
	statuses := map[string]spec.NodeStatus{}
	for _, node := range runNodes {
		statuses[node.NodeID] = node.Status
	}
	if statuses["a"] != spec.NodeStatusFailed {
		t.Fatalf("node a status = %q, want Failed", statuses["a"])
	}
	if statuses["b"] != spec.NodeStatusSkipped {
		t.Fatalf("node b status = %q, want Skipped", statuses["b"])
	}
}

func TestDagEngineKeepsOriginalFailureReasonWhenNotifyNodeTerminalFailsOnFailedNode(t *testing.T) {
	reg := registry.NewMemoryRegistry()
	adapter := &fakeAdapter{failOn: map[string]bool{"a": true}}
	handoffClient := &fakeHandoffClient{notifyErr: fmt.Errorf("notify down")}
	engine := NewDagEngineWithHandoff(reg, adapter, handoffClient)
	specInput := spec.ExecutableRunSpec{
		Run:   spec.RunMetadata{RunID: "run-notify-failed-node", SampleRunID: "sample-notify-failed-node", SubmittedAt: time.Now().UTC(), FailurePolicy: spec.FailurePolicy{Mode: "fail-fast"}},
		Graph: spec.Graph{Nodes: []spec.Node{{NodeID: "a", Image: "busybox:1.36"}}},
	}
	record := spec.RunRecord{RunID: specInput.Run.RunID, Status: spec.RunStatusAccepted, AcceptedAt: time.Now().UTC(), Spec: specInput}
	nodes := []spec.NodeRecord{{RunID: record.RunID, NodeID: "a", Status: spec.NodeStatusPending}}
	if err := reg.CreateRun(context.Background(), record, nodes); err != nil {
		t.Fatalf("CreateRun() error = %v", err)
	}
	if err := engine.Admit(context.Background(), record); err != nil {
		t.Fatalf("Admit() error = %v", err)
	}
	waitForRunStatus(t, reg, record.RunID, spec.RunStatusFailed)

	run, err := reg.GetRun(context.Background(), record.RunID)
	if err != nil {
		t.Fatalf("GetRun() error = %v", err)
	}
	if run.TerminalFailureReason != "backend_wait_error" {
		t.Fatalf("run failureReason = %q, want backend_wait_error", run.TerminalFailureReason)
	}
	runNodes, err := reg.ListNodes(context.Background(), record.RunID)
	if err != nil {
		t.Fatalf("ListNodes() error = %v", err)
	}
	for _, node := range runNodes {
		if node.NodeID == "a" && node.TerminalFailureReason != "backend_wait_error" {
			t.Fatalf("node a failureReason = %q, want backend_wait_error", node.TerminalFailureReason)
		}
	}
	assertEventPresent(t, reg, record.RunID, "node.handoff.notify_failed", "notify_node_terminal_error")
}

func TestDagEngineRecordsLocalityMissAndFallbackSuccess(t *testing.T) {
	reg := registry.NewMemoryRegistry()
	adapter := &fakeAdapter{
		failOn: map[string]bool{},
		observe: &backend.OptionalKueueInfo{
			Observed:    true,
			PodName:     "pod-consume",
			PodNodeName: "node-b",
			Scheduled:   true,
		},
	}
	handoffClient := &fakeHandoffClient{
		response: handoff.ResolveBindingResponse{
			ResolutionStatus: "RESOLVED",
			Decision:         "remote_fetch",
			PlacementIntent: handoff.PlacementIntent{
				Mode:     "preferred_node",
				NodeName: "node-a",
			},
			MaterializationPlan: handoff.MaterializationPlan{
				Mode: "remote_fetch",
				URI:  "http://artifact.local/output",
			},
		},
	}
	engine := NewDagEngineWithHandoff(reg, adapter, handoffClient)
	specInput := spec.ExecutableRunSpec{
		Run: spec.RunMetadata{RunID: "run-locality-miss", SampleRunID: "sample-locality-miss", SubmittedAt: time.Now().UTC(), FailurePolicy: spec.FailurePolicy{Mode: "fail-fast"}},
		Graph: spec.Graph{
			Nodes: []spec.Node{
				{NodeID: "produce", Image: "busybox:1.36"},
				{NodeID: "consume", Image: "busybox:1.36", ArtifactBindings: []spec.ArtifactBinding{{
					BindingName:        "dataset",
					ChildInputName:     "dataset",
					ProducerNodeID:     "produce",
					ProducerOutputName: "report",
					ConsumePolicy:      "RemoteOK",
					Required:           true,
				}}},
			},
			Edges: [][]string{{"produce", "consume"}},
		},
	}
	record := spec.RunRecord{RunID: specInput.Run.RunID, Status: spec.RunStatusAccepted, AcceptedAt: time.Now().UTC(), Spec: specInput}
	nodes := []spec.NodeRecord{{RunID: record.RunID, NodeID: "produce", Status: spec.NodeStatusPending}, {RunID: record.RunID, NodeID: "consume", Status: spec.NodeStatusPending}}
	if err := reg.CreateRun(context.Background(), record, nodes); err != nil {
		t.Fatalf("CreateRun() error = %v", err)
	}
	if err := engine.Admit(context.Background(), record); err != nil {
		t.Fatalf("Admit() error = %v", err)
	}
	waitForRunStatus(t, reg, record.RunID, spec.RunStatusSucceeded)

	runNodes, err := reg.ListNodes(context.Background(), record.RunID)
	if err != nil {
		t.Fatalf("ListNodes() error = %v", err)
	}
	for _, node := range runNodes {
		if node.NodeID != "consume" {
			continue
		}
		if node.Observation.PodNodeName != "node-b" {
			t.Fatalf("consume observation podNodeName = %q, want node-b", node.Observation.PodNodeName)
		}
	}
	adapter.mu.Lock()
	preparedNode, ok := adapter.prepared["consume"]
	adapter.mu.Unlock()
	if !ok {
		t.Fatal("expected prepared node for consume")
	}
	if preparedNode.Placement == nil {
		t.Fatal("expected prepared node placement to be populated")
	}
	if len(preparedNode.Placement.PreferredNodes) != 1 {
		t.Fatalf("preferred placement count = %d, want 1", len(preparedNode.Placement.PreferredNodes))
	}
	if preparedNode.Placement.PreferredNodes[0].NodeName != "node-a" || preparedNode.Placement.PreferredNodes[0].Weight != 100 {
		t.Fatalf("unexpected preferred placement = %+v", preparedNode.Placement.PreferredNodes[0])
	}

	assertEventTypePresent(t, reg, record.RunID, "node.locality.preferred")
	assertEventTypePresent(t, reg, record.RunID, "node.locality.missed")
	assertEventTypePresent(t, reg, record.RunID, "node.locality.fallback_started")
	assertEventTypePresent(t, reg, record.RunID, "node.locality.fallback_succeeded")
	assertEventTypePresent(t, reg, record.RunID, "node.placement.preferred_applied")

	rendered := engine.Metrics().Render()
	for _, want := range []string{
		"jumi_locality_preferred_total 1",
		"jumi_locality_miss_total 1",
		"jumi_locality_fallback_started_total 1",
		"jumi_locality_fallback_succeeded_total 1",
	} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("metrics missing %q in %s", want, rendered)
		}
	}
}

func TestDagEngineCancelRunningNode(t *testing.T) {
	reg := registry.NewMemoryRegistry()
	adapter := &fakeAdapter{
		failOn: map[string]bool{},
		waitCh: map[string]chan struct{}{"a": make(chan struct{})},
	}
	engine := NewDagEngine(reg, adapter)
	specInput := spec.ExecutableRunSpec{
		Run:   spec.RunMetadata{RunID: "run-cancel", SubmittedAt: time.Now().UTC(), FailurePolicy: spec.FailurePolicy{Mode: "fail-fast"}},
		Graph: spec.Graph{Nodes: []spec.Node{{NodeID: "a", Image: "busybox:1.36"}}},
	}
	record := spec.RunRecord{RunID: specInput.Run.RunID, Status: spec.RunStatusAccepted, AcceptedAt: time.Now().UTC(), Spec: specInput}
	nodes := []spec.NodeRecord{{RunID: record.RunID, NodeID: "a", Status: spec.NodeStatusPending}}
	if err := reg.CreateRun(context.Background(), record, nodes); err != nil {
		t.Fatalf("CreateRun() error = %v", err)
	}
	if err := engine.Admit(context.Background(), record); err != nil {
		t.Fatalf("Admit() error = %v", err)
	}
	waitForNodeStatus(t, reg, record.RunID, "a", spec.NodeStatusRunning)
	if err := engine.Cancel(context.Background(), record.RunID, "user_request"); err != nil {
		t.Fatalf("Cancel() error = %v", err)
	}
	waitForRunStatus(t, reg, record.RunID, spec.RunStatusCanceled)
	waitForNodeStatus(t, reg, record.RunID, "a", spec.NodeStatusCanceled)
	adapter.mu.Lock()
	defer adapter.mu.Unlock()
	if !adapter.canceled["a"] {
		t.Fatal("expected adapter cancel for node a")
	}
}

func TestDagEngineKeepsCancellationReasonWhenNotifyNodeTerminalFailsOnCanceledNode(t *testing.T) {
	reg := registry.NewMemoryRegistry()
	adapter := &fakeAdapter{
		failOn: map[string]bool{},
		waitCh: map[string]chan struct{}{"a": make(chan struct{})},
	}
	handoffClient := &fakeHandoffClient{notifyErr: fmt.Errorf("notify down")}
	engine := NewDagEngineWithHandoff(reg, adapter, handoffClient)
	specInput := spec.ExecutableRunSpec{
		Run:   spec.RunMetadata{RunID: "run-notify-canceled-node", SampleRunID: "sample-notify-canceled-node", SubmittedAt: time.Now().UTC(), FailurePolicy: spec.FailurePolicy{Mode: "fail-fast"}},
		Graph: spec.Graph{Nodes: []spec.Node{{NodeID: "a", Image: "busybox:1.36"}}},
	}
	record := spec.RunRecord{RunID: specInput.Run.RunID, Status: spec.RunStatusAccepted, AcceptedAt: time.Now().UTC(), Spec: specInput}
	nodes := []spec.NodeRecord{{RunID: record.RunID, NodeID: "a", Status: spec.NodeStatusPending}}
	if err := reg.CreateRun(context.Background(), record, nodes); err != nil {
		t.Fatalf("CreateRun() error = %v", err)
	}
	if err := engine.Admit(context.Background(), record); err != nil {
		t.Fatalf("Admit() error = %v", err)
	}
	waitForNodeStatus(t, reg, record.RunID, "a", spec.NodeStatusRunning)
	if err := engine.Cancel(context.Background(), record.RunID, "user_request"); err != nil {
		t.Fatalf("Cancel() error = %v", err)
	}
	waitForRunStatus(t, reg, record.RunID, spec.RunStatusCanceled)
	waitForNodeStatus(t, reg, record.RunID, "a", spec.NodeStatusCanceled)

	run, err := reg.GetRun(context.Background(), record.RunID)
	if err != nil {
		t.Fatalf("GetRun() error = %v", err)
	}
	if run.TerminalFailureReason != "user_request" {
		t.Fatalf("run failureReason = %q, want user_request", run.TerminalFailureReason)
	}
	runNodes, err := reg.ListNodes(context.Background(), record.RunID)
	if err != nil {
		t.Fatalf("ListNodes() error = %v", err)
	}
	for _, node := range runNodes {
		if node.NodeID == "a" && node.TerminalFailureReason != "user_request" {
			t.Fatalf("node a failureReason = %q, want user_request", node.TerminalFailureReason)
		}
	}
	assertEventPresent(t, reg, record.RunID, "node.handoff.notify_failed", "notify_node_terminal_error")
}

func waitForRunStatus(t *testing.T, reg registry.Registry, runID string, want spec.RunStatus) {
	t.Helper()
	waitForRunStatusWithin(t, reg, runID, want, 3*time.Second)
}

func waitForRunStatusWithin(t *testing.T, reg registry.Registry, runID string, want spec.RunStatus, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		run, err := reg.GetRun(context.Background(), runID)
		if err == nil && run.Status == want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	run, err := reg.GetRun(context.Background(), runID)
	if err != nil {
		t.Fatalf("GetRun() error = %v", err)
	}
	t.Fatalf("run status = %q, want %q", run.Status, want)
}

func waitForNodeStatus(t *testing.T, reg registry.Registry, runID, nodeID string, want spec.NodeStatus) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		nodes, err := reg.ListNodes(context.Background(), runID)
		if err == nil {
			for _, node := range nodes {
				if node.NodeID == nodeID && node.Status == want {
					return
				}
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	nodes, err := reg.ListNodes(context.Background(), runID)
	if err != nil {
		t.Fatalf("ListNodes() error = %v", err)
	}
	for _, node := range nodes {
		if node.NodeID == nodeID {
			t.Fatalf("node %s status = %q, want %q", nodeID, node.Status, want)
		}
	}
	t.Fatalf("node %s not found", nodeID)
}

func assertEventPresent(t *testing.T, reg registry.Registry, runID string, eventType string, failureReason string) {
	t.Helper()
	events, err := reg.ListEvents(context.Background(), runID, 0)
	if err != nil {
		t.Fatalf("ListEvents() error = %v", err)
	}
	for _, event := range events {
		if event.Type == eventType && event.FailureReason == failureReason {
			return
		}
	}
	t.Fatalf("event type=%q failureReason=%q not found", eventType, failureReason)
}

func assertEventAbsent(t *testing.T, reg registry.Registry, runID string, eventType string) {
	t.Helper()
	events, err := reg.ListEvents(context.Background(), runID, 0)
	if err != nil {
		t.Fatalf("ListEvents() error = %v", err)
	}
	for _, event := range events {
		if event.Type == eventType {
			t.Fatalf("unexpected event type=%q found", eventType)
		}
	}
}

func assertEventTypePresent(t *testing.T, reg registry.Registry, runID string, eventType string) {
	t.Helper()
	events, err := reg.ListEvents(context.Background(), runID, 0)
	if err != nil {
		t.Fatalf("ListEvents() error = %v", err)
	}
	for _, event := range events {
		if event.Type == eventType {
			return
		}
	}
	t.Fatalf("event type=%q not found", eventType)
}
