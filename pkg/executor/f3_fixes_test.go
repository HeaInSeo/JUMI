package executor

import (
	"context"
	"testing"
	"time"

	"github.com/HeaInSeo/JUMI/pkg/backend"
	"github.com/HeaInSeo/JUMI/pkg/registry"
	"github.com/HeaInSeo/JUMI/pkg/spec"
)

// startErrAdapter overrides StartNode to return an error, simulating a lost
// StartNode response after the submission fence was durably crossed (F2).
type startErrAdapter struct {
	*fakeAdapter
	startErr error
}

func (a *startErrAdapter) StartNode(_ context.Context, _ backend.PreparedNode) (backend.Handle, error) {
	return nil, a.startErr
}

func waitForEventType(t *testing.T, reg registry.Registry, runID, eventType string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		events, err := reg.ListEvents(context.Background(), runID, 0)
		if err == nil {
			for _, ev := range events {
				if ev.Type == eventType {
					return
				}
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("event type=%q not observed within %s", eventType, timeout)
}

// TestF2_PostFenceStartErrorIsUnresolved verifies that a StartNode error AFTER
// the submission fence is durably persisted is treated as an unknown outcome:
// the Attempt is left non-terminal (no replacement Attempt is allocated) and the
// Run is left non-terminal/recoverable rather than being terminalized as Failed.
func TestF2_PostFenceStartErrorIsUnresolved(t *testing.T) {
	reg := registry.NewMemoryRegistry()
	adapter := &startErrAdapter{fakeAdapter: &fakeAdapter{failOn: map[string]bool{}}, startErr: context.DeadlineExceeded}
	engine := NewDagEngine(reg, adapter)

	runID := "run-f2-starterr"
	specInput := spec.ExecutableRunSpec{
		Run:   spec.RunMetadata{RunID: runID, SubmittedAt: time.Now().UTC(), FailurePolicy: spec.FailurePolicy{Mode: "fail-fast"}},
		Graph: spec.Graph{Nodes: []spec.Node{{NodeID: "a", Image: "busybox:1.36"}}},
	}
	record := spec.RunRecord{RunID: runID, Status: spec.RunStatusAccepted, AcceptedAt: time.Now().UTC(), Spec: specInput}
	nodes := []spec.NodeRecord{{RunID: runID, NodeID: "a", Status: spec.NodeStatusPending}}
	if err := reg.CreateRun(context.Background(), record, nodes); err != nil {
		t.Fatalf("CreateRun: %v", err)
	}
	if err := engine.Admit(context.Background(), record); err != nil {
		t.Fatalf("Admit: %v", err)
	}

	// The node must surface the unresolved recovery event, and the run the
	// unresolved run event — never a terminal failure.
	waitForEventType(t, reg, runID, "node.recovery.unresolved", 3*time.Second)
	waitForEventType(t, reg, runID, "run.recovery.unresolved", 3*time.Second)

	// Give any (incorrect) extra allocation a chance to happen before asserting.
	time.Sleep(100 * time.Millisecond)

	run, err := reg.GetRun(context.Background(), runID)
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	if run.Status.IsTerminal() {
		t.Fatalf("run status = %q, want non-terminal (recoverable) after unresolved StartNode", run.Status)
	}

	node, err := reg.GetNode(context.Background(), runID, "a")
	if err != nil {
		t.Fatalf("GetNode: %v", err)
	}
	// Exactly one Attempt was allocated; no replacement Attempt for the
	// possibly-live backend Job.
	if node.AttemptCount != 1 {
		t.Fatalf("AttemptCount = %d, want 1 (no replacement Attempt after fence-crossed StartNode error)", node.AttemptCount)
	}
	if node.Status.IsTerminal() {
		t.Fatalf("node status = %q, want non-terminal after unresolved StartNode", node.Status)
	}
	attempts, err := reg.ListAttempts(context.Background(), runID, "a")
	if err != nil {
		t.Fatalf("ListAttempts: %v", err)
	}
	if len(attempts) != 1 {
		t.Fatalf("attempt records = %d, want 1", len(attempts))
	}
	if attempts[0].Status.IsTerminal() {
		t.Fatalf("attempt status = %q, want non-terminal (unresolved)", attempts[0].Status)
	}
}

// TestF3_ReleasingReattach verifies that a node crashed after PersistBackendHandle
// but before the flip to Running (durable status still Releasing) is reattached
// rather than abandoned — the genuinely-submitted Job is observed to completion.
func TestF3_ReleasingReattach(t *testing.T) {
	reg := registry.NewMemoryRegistry()
	adapter := &persistableAdapter{fakeAdapter: &fakeAdapter{failOn: map[string]bool{}}}

	runID := "run-f3-releasing"
	specInput := spec.ExecutableRunSpec{
		Run:   spec.RunMetadata{RunID: runID, SubmittedAt: time.Now().UTC(), FailurePolicy: spec.FailurePolicy{Mode: "fail-fast"}},
		Graph: spec.Graph{Nodes: []spec.Node{{NodeID: "a", Image: "busybox:1.36"}}},
	}
	record := spec.RunRecord{RunID: runID, Status: spec.RunStatusRunning, AcceptedAt: time.Now().UTC(), Spec: specInput}
	attemptID := runID + "-a-attempt-1"
	handleData, _ := adapter.MarshalHandle(fakeHandle{nodeID: "a"})
	// Durable state: handle persisted, but node status is still Releasing.
	nodes := []spec.NodeRecord{{
		RunID:                    runID,
		NodeID:                   "a",
		Status:                   spec.NodeStatusReleasing,
		AttemptCount:             1,
		CurrentAttemptID:         attemptID,
		CurrentAttemptHandleJSON: string(handleData),
	}}
	if err := reg.CreateRun(context.Background(), record, nodes); err != nil {
		t.Fatalf("CreateRun: %v", err)
	}

	engine := NewDagEngine(reg, adapter)
	if err := engine.Admit(context.Background(), record); err != nil {
		t.Fatalf("Admit: %v", err)
	}
	waitForRunStatus(t, reg, runID, spec.RunStatusSucceeded)

	node, err := reg.GetNode(context.Background(), runID, "a")
	if err != nil {
		t.Fatalf("GetNode: %v", err)
	}
	if node.AttemptCount != 1 {
		t.Fatalf("AttemptCount = %d after Releasing reattach, want 1 (no new attempt)", node.AttemptCount)
	}
	assertEventTypePresent(t, reg, runID, "node.recovery.reattached")
}

// TestF4_UnresolvedRunNotFinalizedAndRecoverable verifies that a node whose
// backend truth is unresolved (fence crossed, no handle -> resolve-by-identity)
// does NOT terminalize the Run as Failed, does NOT strand downstream Pending
// nodes as Skipped, and is re-picked up by a later reconcile sweep.
func TestF4_UnresolvedRunNotFinalizedAndRecoverable(t *testing.T) {
	reg := registry.NewMemoryRegistry()
	adapter := &fakeAdapter{failOn: map[string]bool{}}
	engine := NewDagEngine(reg, adapter)

	runID := "run-f4-unresolved"
	specInput := spec.ExecutableRunSpec{
		Run: spec.RunMetadata{RunID: runID, SubmittedAt: time.Now().UTC(), FailurePolicy: spec.FailurePolicy{Mode: "fail-fast"}},
		Graph: spec.Graph{
			Nodes: []spec.Node{{NodeID: "a", Image: "busybox:1.36"}, {NodeID: "b", Image: "busybox:1.36"}},
			Edges: [][]string{{"a", "b"}},
		},
	}
	record := spec.RunRecord{RunID: runID, Status: spec.RunStatusRunning, AcceptedAt: time.Now().UTC(), Spec: specInput}
	attemptID := runID + "-a-attempt-1"
	nodes := []spec.NodeRecord{
		{RunID: runID, NodeID: "a", Status: spec.NodeStatusStarting, AttemptCount: 1, CurrentAttemptID: attemptID},
		{RunID: runID, NodeID: "b", Status: spec.NodeStatusPending},
	}
	if err := reg.CreateRun(context.Background(), record, nodes); err != nil {
		t.Fatalf("CreateRun: %v", err)
	}
	// Attempt crossed the submission fence but persisted no handle -> ResolveByIdentity.
	if err := reg.UpsertAttempt(context.Background(), spec.AttemptRecord{RunID: runID, NodeID: "a", AttemptID: attemptID, Status: spec.AttemptStatusPrepared}); err != nil {
		t.Fatalf("UpsertAttempt: %v", err)
	}
	if err := reg.PersistSubmissionFence(context.Background(), runID, "a", attemptID, time.Now().UTC()); err != nil {
		t.Fatalf("PersistSubmissionFence: %v", err)
	}

	if err := engine.Admit(context.Background(), record); err != nil {
		t.Fatalf("Admit: %v", err)
	}
	waitForEventType(t, reg, runID, "run.recovery.unresolved", 3*time.Second)
	time.Sleep(100 * time.Millisecond)

	run, err := reg.GetRun(context.Background(), runID)
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	if run.Status.IsTerminal() {
		t.Fatalf("run status = %q, want non-terminal (recoverable), not Failed", run.Status)
	}
	// Downstream node must remain Pending (recoverable), NOT permanently Skipped.
	nodeB, err := reg.GetNode(context.Background(), runID, "b")
	if err != nil {
		t.Fatalf("GetNode(b): %v", err)
	}
	if nodeB.Status != spec.NodeStatusPending {
		t.Fatalf("node b status = %q, want Pending (not stranded as Skipped)", nodeB.Status)
	}
	// Attempt of a must remain non-terminal so no replacement is ever allocated.
	nodeA, err := reg.GetNode(context.Background(), runID, "a")
	if err != nil {
		t.Fatalf("GetNode(a): %v", err)
	}
	if nodeA.AttemptCount != 1 {
		t.Fatalf("node a AttemptCount = %d, want 1 (no replacement Attempt)", nodeA.AttemptCount)
	}

	// A later reconcile sweep must re-pick up the still-non-terminal run.
	engine2 := NewDagEngine(reg, adapter)
	if err := engine2.Recover(context.Background()); err != nil {
		t.Fatalf("Recover: %v", err)
	}
	waitForEventType(t, reg, runID, "run.recovery.resumed", 3*time.Second)
}

// TestF5_CanceledNonterminalRecovery verifies that a Run marked Canceled while a
// node is still non-terminal (accepted cancel that never reached terminal truth)
// is re-driven on restart: the backend Job is canceled and the node reaches a
// terminal state, consuming the persisted cancellation intent.
func TestF5_CanceledNonterminalRecovery(t *testing.T) {
	reg := registry.NewMemoryRegistry()
	inner := &fakeAdapter{failOn: map[string]bool{"a": true}}
	adapter := &persistableAdapter{fakeAdapter: inner}

	runID := "run-f5-cancel"
	specInput := spec.ExecutableRunSpec{
		Run:   spec.RunMetadata{RunID: runID, SubmittedAt: time.Now().UTC(), FailurePolicy: spec.FailurePolicy{Mode: "fail-fast"}},
		Graph: spec.Graph{Nodes: []spec.Node{{NodeID: "a", Image: "busybox:1.36"}}},
	}
	// Run is terminal (Canceled) but node "a" is still Running with a live handle.
	record := spec.RunRecord{RunID: runID, Status: spec.RunStatusCanceled, AcceptedAt: time.Now().UTC(), TerminalStopCause: "canceled", TerminalFailureReason: "cancellation_requested", Spec: specInput}
	attemptID := runID + "-a-attempt-1"
	handleData, _ := adapter.MarshalHandle(fakeHandle{nodeID: "a"})
	nodes := []spec.NodeRecord{{
		RunID:                     runID,
		NodeID:                    "a",
		Status:                    spec.NodeStatusRunning,
		AttemptCount:              1,
		CurrentAttemptID:          attemptID,
		CurrentAttemptHandleJSON:  string(handleData),
		CurrentBottleneckLocation: "canceling",
	}}
	if err := reg.CreateRun(context.Background(), record, nodes); err != nil {
		t.Fatalf("CreateRun: %v", err)
	}
	if err := reg.UpsertAttempt(context.Background(), spec.AttemptRecord{RunID: runID, NodeID: "a", AttemptID: attemptID, Status: spec.AttemptStatusStarted, BackendHandleJSON: string(handleData)}); err != nil {
		t.Fatalf("UpsertAttempt: %v", err)
	}
	// Persisted cancellation intent — the fact F5 recovery must consume.
	if err := reg.PersistCancellationIntent(context.Background(), runID, "a", attemptID, time.Now().UTC(), "cancellation_requested"); err != nil {
		t.Fatalf("PersistCancellationIntent: %v", err)
	}

	engine := NewDagEngine(reg, adapter)
	if err := engine.Recover(context.Background()); err != nil {
		t.Fatalf("Recover: %v", err)
	}

	waitForNodeStatus(t, reg, runID, "a", spec.NodeStatusCanceled)

	inner.mu.Lock()
	canceled := inner.canceled["a"]
	inner.mu.Unlock()
	if !canceled {
		t.Fatal("expected CancelNode to be invoked on the reattached backend Job during cancel recovery")
	}

	run, err := reg.GetRun(context.Background(), runID)
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	if run.Status != spec.RunStatusCanceled {
		t.Fatalf("run status = %q, want Canceled (recovery must not un-terminalize)", run.Status)
	}
	assertEventTypePresent(t, reg, runID, "run.recovery.cancel_resumed")
}

// TestF6_RetryBudgetHoldsAcrossRestart verifies the total-attempt cap is derived
// from the durable AttemptCount, so attempts already made before a restart count
// against MaxAttempts (the budget is NOT reset on each RunE entry).
func TestF6_RetryBudgetHoldsAcrossRestart(t *testing.T) {
	reg := registry.NewMemoryRegistry()
	adapter := &fakeAdapter{failOn: map[string]bool{"a": true}}
	engine := NewDagEngine(reg, adapter)

	runID := "run-f6-budget"
	specInput := spec.ExecutableRunSpec{
		Run: spec.RunMetadata{RunID: runID, SubmittedAt: time.Now().UTC(), FailurePolicy: spec.FailurePolicy{Mode: "fail-fast"}},
		Graph: spec.Graph{Nodes: []spec.Node{{
			NodeID: "a", Image: "busybox:1.36",
			RetryPolicy: spec.RetryPolicy{MaxAttempts: 3},
		}}},
	}
	// Restart state: 2 attempts already made (AttemptCount=2), node reset to
	// Pending for the next retry. Only ONE more attempt is within the cap of 3.
	record := spec.RunRecord{RunID: runID, Status: spec.RunStatusRunning, AcceptedAt: time.Now().UTC(), Spec: specInput}
	nodes := []spec.NodeRecord{{RunID: runID, NodeID: "a", Status: spec.NodeStatusPending, AttemptCount: 2, CurrentAttemptID: ""}}
	if err := reg.CreateRun(context.Background(), record, nodes); err != nil {
		t.Fatalf("CreateRun: %v", err)
	}
	if err := engine.Admit(context.Background(), record); err != nil {
		t.Fatalf("Admit: %v", err)
	}
	waitForRunStatus(t, reg, runID, spec.RunStatusFailed)

	node, err := reg.GetNode(context.Background(), runID, "a")
	if err != nil {
		t.Fatalf("GetNode: %v", err)
	}
	if node.AttemptCount != 3 {
		t.Fatalf("AttemptCount = %d after restart, want 3 (cap of MaxAttempts=3 must hold across restart)", node.AttemptCount)
	}
	if node.Status != spec.NodeStatusFailed {
		t.Fatalf("node status = %q, want Failed", node.Status)
	}
}
