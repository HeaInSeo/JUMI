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
	startDelay time.Duration
}

func (f *fakeAdapter) PrepareNode(_ context.Context, _ spec.RunRecord, node spec.Node) (backend.PreparedNode, error) {
	return fakePrepared{nodeID: node.NodeID}, nil
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
