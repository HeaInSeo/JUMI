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
	"github.com/HeaInSeo/JUMI/pkg/registry"
	"github.com/HeaInSeo/JUMI/pkg/spec"
)

type fakePrepared struct {
	nodeID string
}

type fakeHandle struct {
	nodeID string
}

type fakeAdapter struct {
	mu         sync.Mutex
	order      []string
	failOn     map[string]bool
	waitCh     map[string]chan struct{}
	canceled   map[string]bool
	prepared   map[string]spec.Node
	startDelay time.Duration
}

type fakeHandoffClient struct {
	mu                 sync.Mutex
	requests           []handoff.ResolveBindingRequest
	registerRequests   []handoff.RegisterArtifactRequest
	notifyRequests     []handoff.NotifyNodeTerminalRequest
	finalizeRequests   []handoff.FinalizeSampleRunRequest
	evaluateRequests   []handoff.EvaluateGCRequest
	response           handoff.ResolveBindingResponse
	err                error
}

func (f *fakeHandoffClient) ResolveBinding(_ context.Context, req handoff.ResolveBindingRequest) (handoff.ResolveBindingResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests = append(f.requests, req)
	if f.err != nil {
		return handoff.ResolveBindingResponse{}, f.err
	}
	if f.response.ResolutionStatus == "" {
		return handoff.ResolveBindingResponse{ResolutionStatus: "RESOLVED", Decision: "remote_fetch", RequiresMaterialization: true}, nil
	}
	return f.response, nil
}

func (f *fakeHandoffClient) RegisterArtifact(_ context.Context, req handoff.RegisterArtifactRequest) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.registerRequests = append(f.registerRequests, req)
	return nil
}

func (f *fakeHandoffClient) NotifyNodeTerminal(_ context.Context, req handoff.NotifyNodeTerminalRequest) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.notifyRequests = append(f.notifyRequests, req)
	return nil
}

func (f *fakeHandoffClient) FinalizeSampleRun(_ context.Context, req handoff.FinalizeSampleRunRequest) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.finalizeRequests = append(f.finalizeRequests, req)
	return nil
}

func (f *fakeHandoffClient) EvaluateGC(_ context.Context, req handoff.EvaluateGCRequest) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.evaluateRequests = append(f.evaluateRequests, req)
	return nil
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
		response: handoff.ResolveBindingResponse{ResolutionStatus: "RESOLVED", Decision: "remote_fetch", RequiresMaterialization: true},
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
	if preparedNode.Env["JUMI_INPUT_DATASET_REQUIRES_MATERIALIZATION"] != "true" {
		t.Fatalf("requires materialization env = %q, want true", preparedNode.Env["JUMI_INPUT_DATASET_REQUIRES_MATERIALIZATION"])
	}
	if got := engine.Metrics().Render(); !strings.Contains(got, "jumi_input_resolve_requests_total 1") {
		t.Fatalf("expected resolve metric in render: %s", got)
	}
	if len(handoffClient.notifyRequests) == 0 {
		t.Fatal("expected node terminal notification")
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
	if handoffClient.registerRequests[0].URI == "" {
		t.Fatal("expected non-empty output URI")
	}
	if got := engine.Metrics().Render(); !strings.Contains(got, "jumi_artifacts_registered_total 2") {
		t.Fatalf("expected artifact register metric in render: %s", got)
	}
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
	return fakeHandle{nodeID: p.nodeID}, nil
}

func (f *fakeAdapter) ObserveNode(_ context.Context, _ backend.Handle) (*backend.OptionalKueueInfo, error) {
	return nil, nil
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

func waitForRunStatus(t *testing.T, reg registry.Registry, runID string, want spec.RunStatus) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
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
