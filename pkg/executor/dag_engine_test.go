package executor

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/HeaInSeo/JUMI/pkg/backend"
	"github.com/HeaInSeo/JUMI/pkg/registry"
	"github.com/HeaInSeo/JUMI/pkg/spec"
)

type fakeAdapter struct {
	mu     sync.Mutex
	order  []string
	failOn map[string]bool
}

func (f *fakeAdapter) ExecuteNode(_ context.Context, _ spec.RunRecord, node spec.Node) (backend.ExecutionResult, error) {
	f.mu.Lock()
	f.order = append(f.order, node.NodeID)
	f.mu.Unlock()
	if f.failOn[node.NodeID] {
		return backend.ExecutionResult{TerminalStopCause: "failed", TerminalFailureReason: "backend_wait_error"}, fmt.Errorf("forced failure")
	}
	return backend.ExecutionResult{Succeeded: true, TerminalStopCause: "finished"}, nil
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
