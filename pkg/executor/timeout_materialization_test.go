package executor

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/HeaInSeo/JUMI/pkg/backend"
	"github.com/HeaInSeo/JUMI/pkg/registry"
	"github.com/HeaInSeo/JUMI/pkg/spec"
)

func singleNodeRun(t *testing.T, reg registry.Registry, runID string, node spec.Node) spec.RunRecord {
	t.Helper()
	specInput := spec.ExecutableRunSpec{
		Run:   spec.RunMetadata{RunID: runID, SubmittedAt: time.Now().UTC(), FailurePolicy: spec.FailurePolicy{Mode: "fail-fast"}},
		Graph: spec.Graph{Nodes: []spec.Node{node}},
	}
	record := spec.RunRecord{RunID: runID, Status: spec.RunStatusAccepted, AcceptedAt: time.Now().UTC(), Spec: specInput}
	if err := reg.CreateRun(context.Background(), record, []spec.NodeRecord{{RunID: runID, NodeID: node.NodeID, Status: spec.NodeStatusPending}}); err != nil {
		t.Fatalf("CreateRun() error = %v", err)
	}
	return record
}

func nodeRecordFor(t *testing.T, reg registry.Registry, runID, nodeID string) spec.NodeRecord {
	t.Helper()
	n, err := reg.GetNode(context.Background(), runID, nodeID)
	if err != nil {
		t.Fatalf("GetNode() error = %v", err)
	}
	return n
}

// D3 (issue #2 F3): an expired TimeoutPolicy is recorded as a timeout — Failed /
// deadline_exceeded — not as a user cancellation. The backend workload is asked to stop,
// and no replacement Attempt is opened even with retry budget left (user code may have
// run; Q30-R1).
func TestD3_NodeTimeoutIsNotRecordedAsCancellation(t *testing.T) {
	reg := registry.NewMemoryRegistry()
	adapter := &fakeAdapter{failOn: map[string]bool{}, waitCh: map[string]chan struct{}{"a": make(chan struct{})}}
	engine := NewDagEngine(reg, adapter)
	record := singleNodeRun(t, reg, "run-node-timeout", spec.Node{
		NodeID: "a", Image: "busybox:1.36",
		TimeoutPolicy: spec.TimeoutPolicy{Seconds: 1},
		RetryPolicy:   spec.RetryPolicy{MaxAttempts: 3},
	})
	if err := engine.Admit(context.Background(), record); err != nil {
		t.Fatalf("Admit() error = %v", err)
	}
	waitForRunStatusWithin(t, reg, record.RunID, spec.RunStatusFailed, 5*time.Second)

	node := nodeRecordFor(t, reg, record.RunID, "a")
	if node.Status != spec.NodeStatusFailed || node.TerminalStopCause != "failed" || node.TerminalFailureReason != nodeTimeoutFailureReason {
		t.Fatalf("timed-out node = status %q cause %q reason %q, want Failed/failed/%s",
			node.Status, node.TerminalStopCause, node.TerminalFailureReason, nodeTimeoutFailureReason)
	}
	run, err := reg.GetRun(context.Background(), record.RunID)
	if err != nil {
		t.Fatalf("GetRun() error = %v", err)
	}
	if run.TerminalStopCause == "canceled" || run.TerminalFailureReason != nodeTimeoutFailureReason {
		t.Fatalf("run = cause %q reason %q, want a failed run with reason %s", run.TerminalStopCause, run.TerminalFailureReason, nodeTimeoutFailureReason)
	}
	attempts, err := reg.ListAttempts(context.Background(), record.RunID, "a")
	if err != nil {
		t.Fatalf("ListAttempts() error = %v", err)
	}
	if len(attempts) != 1 {
		t.Fatalf("attempts = %d, want 1 (a timeout must not open a replacement attempt)", len(attempts))
	}
	if attempts[0].TerminalStopCause == "canceled" || attempts[0].TerminalFailureReason != nodeTimeoutFailureReason {
		t.Fatalf("attempt = cause %q reason %q, want %s", attempts[0].TerminalStopCause, attempts[0].TerminalFailureReason, nodeTimeoutFailureReason)
	}
	adapter.mu.Lock()
	stopped := adapter.canceled["a"]
	adapter.mu.Unlock()
	if !stopped {
		t.Fatal("timed-out backend workload was not asked to stop")
	}
}

// D3 counterpart: a real user cancel of a node that also has a TimeoutPolicy keeps the
// existing cancellation values.
func TestD3_UserCancelWithTimeoutPolicyStaysCanceled(t *testing.T) {
	reg := registry.NewMemoryRegistry()
	adapter := &fakeAdapter{failOn: map[string]bool{}, waitCh: map[string]chan struct{}{"a": make(chan struct{})}}
	engine := NewDagEngine(reg, adapter)
	record := singleNodeRun(t, reg, "run-cancel-with-timeout", spec.Node{
		NodeID: "a", Image: "busybox:1.36",
		TimeoutPolicy: spec.TimeoutPolicy{Seconds: 600},
	})
	if err := engine.Admit(context.Background(), record); err != nil {
		t.Fatalf("Admit() error = %v", err)
	}
	waitForNodeStatus(t, reg, record.RunID, "a", spec.NodeStatusRunning)
	if err := engine.Cancel(context.Background(), record.RunID, "user_request"); err != nil {
		t.Fatalf("Cancel() error = %v", err)
	}
	waitForRunStatus(t, reg, record.RunID, spec.RunStatusCanceled)
	waitForNodeStatus(t, reg, record.RunID, "a", spec.NodeStatusCanceled)
	node := nodeRecordFor(t, reg, record.RunID, "a")
	if node.TerminalStopCause != "canceled" || node.TerminalFailureReason == nodeTimeoutFailureReason {
		t.Fatalf("user-canceled node = cause %q reason %q, want canceled and not a timeout", node.TerminalStopCause, node.TerminalFailureReason)
	}
}

// D2 executor mapping: a backend that fails closed on unavailable input materialization
// terminalizes the node with a specific reason, deterministically — no realization
// re-attempt is spent on it.
func TestD2_InputMaterializationUnavailableFailsWithoutRealizationRetry(t *testing.T) {
	reg := registry.NewMemoryRegistry()
	adapter := &fakeAdapter{
		failOn:       map[string]bool{},
		prepareErrOn: map[string]error{"a": fmt.Errorf("%w: test", backend.ErrInputMaterializationUnavailable)},
	}
	engine := NewDagEngine(reg, adapter)
	record := singleNodeRun(t, reg, "run-materialization-unavailable", spec.Node{NodeID: "a", Image: "busybox:1.36"})
	if err := engine.Admit(context.Background(), record); err != nil {
		t.Fatalf("Admit() error = %v", err)
	}
	waitForRunStatus(t, reg, record.RunID, spec.RunStatusFailed)

	node := nodeRecordFor(t, reg, record.RunID, "a")
	if node.TerminalFailureReason != materializationFailureRuntimeUnavailable {
		t.Fatalf("node reason = %q, want %s", node.TerminalFailureReason, materializationFailureRuntimeUnavailable)
	}
	adapter.mu.Lock()
	calls := adapter.prepareCalls["a"]
	started := len(adapter.order)
	adapter.mu.Unlock()
	if calls != 1 {
		t.Fatalf("PrepareNode calls = %d, want 1 (deterministic failure must not be re-realized)", calls)
	}
	if started != 0 {
		t.Fatalf("StartNode called %d times for a node that failed closed before submission", started)
	}
}
