package executor

import (
	"context"
	"testing"
	"time"

	"github.com/HeaInSeo/JUMI/pkg/registry"
	"github.com/HeaInSeo/JUMI/pkg/spec"
)

// admitBindingRun admits a two-node run (b consumes a's output) and waits for it
// to succeed.
func admitBindingRun(t *testing.T, reg registry.Registry, engine *DagEngine, runID, sampleRunID string) {
	t.Helper()
	specInput := spec.ExecutableRunSpec{
		Run: spec.RunMetadata{RunID: runID, SampleRunID: sampleRunID, SubmittedAt: time.Now().UTC(), FailurePolicy: spec.FailurePolicy{Mode: "fail-fast"}},
		Graph: spec.Graph{
			Nodes: []spec.Node{
				{NodeID: "a", Image: "busybox:1.36"},
				{NodeID: "b", Image: "busybox:1.36", ArtifactBindings: []spec.ArtifactBinding{{
					BindingName:        "dataset",
					ChildInputName:     "dataset",
					ProducerNodeID:     "a",
					ProducerOutputName: "output",
					Required:           true,
				}}},
			},
			Edges: [][]string{{"a", "b"}},
		},
	}
	record := spec.RunRecord{RunID: runID, Status: spec.RunStatusAccepted, AcceptedAt: time.Now().UTC(), Spec: specInput}
	nodes := []spec.NodeRecord{{RunID: runID, NodeID: "a", Status: spec.NodeStatusPending}, {RunID: runID, NodeID: "b", Status: spec.NodeStatusPending}}
	if err := reg.CreateRun(context.Background(), record, nodes); err != nil {
		t.Fatalf("CreateRun(%s) error = %v", runID, err)
	}
	if err := engine.Admit(context.Background(), record); err != nil {
		t.Fatalf("Admit(%s) error = %v", runID, err)
	}
	waitForRunStatus(t, reg, runID, spec.RunStatusSucceeded)
}

// F4 / FD-DATA-01 R09: two Runs of the same Sample reach artifact-handoff with
// their own RunID on every call (resolve, terminal notify, finalize, GC), so
// their keys, terminal partitions and GC scopes stay separate.
func TestDagEngine_SameSampleRunsKeepDistinctRunIDs(t *testing.T) {
	reg := registry.NewMemoryRegistry()
	adapter := &fakeAdapter{failOn: map[string]bool{}}
	handoffClient := &fakeHandoffClient{}
	engine := NewDagEngineWithHandoff(reg, adapter, handoffClient)

	admitBindingRun(t, reg, engine, "run-r1", "sample-shared")
	admitBindingRun(t, reg, engine, "run-r2", "sample-shared")

	handoffClient.mu.Lock()
	defer handoffClient.mu.Unlock()
	perRun := map[string]int{}
	for _, req := range handoffClient.requests {
		if req.SampleRunID != "sample-shared" {
			t.Fatalf("resolve sampleRunID = %q, want sample-shared", req.SampleRunID)
		}
		perRun["resolve/"+req.RunID]++
	}
	for _, req := range handoffClient.notifyRequests {
		perRun["notify/"+req.RunID]++
	}
	for _, req := range handoffClient.finalizeRequests {
		if req.SampleRunID != "sample-shared" {
			t.Fatalf("finalize sampleRunID = %q, want sample-shared", req.SampleRunID)
		}
		perRun["finalize/"+req.RunID]++
	}
	for _, req := range handoffClient.evaluateRequests {
		perRun["gc/"+req.RunID]++
	}
	want := map[string]int{
		"resolve/run-r1": 1, "resolve/run-r2": 1,
		"notify/run-r1": 2, "notify/run-r2": 2,
		"finalize/run-r1": 1, "finalize/run-r2": 1,
		"gc/run-r1": 1, "gc/run-r2": 1,
	}
	if len(perRun) != len(want) {
		t.Fatalf("artifact-handoff calls by run = %v, want %v", perRun, want)
	}
	for k, n := range want {
		if perRun[k] != n {
			t.Fatalf("artifact-handoff calls by run = %v, want %v", perRun, want)
		}
	}
}

// A run without a Sample sends an empty SampleRunID: the RunID is never copied
// into the Sample grouping field.
func TestDagEngine_RunWithoutSampleDoesNotReuseRunIDAsSample(t *testing.T) {
	reg := registry.NewMemoryRegistry()
	adapter := &fakeAdapter{failOn: map[string]bool{}}
	handoffClient := &fakeHandoffClient{}
	engine := NewDagEngineWithHandoff(reg, adapter, handoffClient)

	admitBindingRun(t, reg, engine, "run-nosample", "")

	handoffClient.mu.Lock()
	defer handoffClient.mu.Unlock()
	if len(handoffClient.requests) != 1 || handoffClient.requests[0].RunID != "run-nosample" || handoffClient.requests[0].SampleRunID != "" {
		t.Fatalf("resolve requests = %+v, want runID run-nosample and empty sampleRunID", handoffClient.requests)
	}
	if len(handoffClient.finalizeRequests) != 1 || handoffClient.finalizeRequests[0].RunID != "run-nosample" || handoffClient.finalizeRequests[0].SampleRunID != "" {
		t.Fatalf("finalize requests = %+v, want runID run-nosample and empty sampleRunID", handoffClient.finalizeRequests)
	}
}
