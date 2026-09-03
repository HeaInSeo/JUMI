package executor

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/HeaInSeo/JUMI/pkg/backend"
	"github.com/HeaInSeo/JUMI/pkg/registry"
	"github.com/HeaInSeo/JUMI/pkg/spec"
)

// resolverAdapter is a fake backend that implements backend.AttemptResolver and
// backend.HandlePersister. Its Create side effect (StartNode) is COUNTED so tests
// can assert recovery performs a read-only resolve and never creates a Job.
type resolverAdapter struct {
	*persistableAdapter

	mu          sync.Mutex
	createCount int
	resolveCall int

	outcome backend.ResolveOutcome
	handle  backend.Handle
	err     error
}

func newResolverAdapter(inner *fakeAdapter, outcome backend.ResolveOutcome, handle backend.Handle, err error) *resolverAdapter {
	return &resolverAdapter{
		persistableAdapter: &persistableAdapter{fakeAdapter: inner},
		outcome:            outcome,
		handle:             handle,
		err:                err,
	}
}

// StartNode counts the create side effect and delegates. Recovery must never call it.
func (a *resolverAdapter) StartNode(ctx context.Context, prepared backend.PreparedNode) (backend.Handle, error) {
	a.mu.Lock()
	a.createCount++
	a.mu.Unlock()
	return a.persistableAdapter.StartNode(ctx, prepared)
}

// ResolveByIdentity is the read-only find seam under test. It performs no create.
func (a *resolverAdapter) ResolveByIdentity(_ context.Context, _ spec.RunRecord, _ spec.Node, _ string) (backend.Handle, backend.ResolveOutcome, error) {
	a.mu.Lock()
	a.resolveCall++
	a.mu.Unlock()
	return a.handle, a.outcome, a.err
}

func (a *resolverAdapter) creates() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.createCount
}

func (a *resolverAdapter) resolves() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.resolveCall
}

// seedFenceCrossedRun creates a run whose node "a" crossed the submission fence
// (SubmissionWindowOpenedAt set on the Attempt) but persisted no backend handle,
// i.e. ClassifyReconcile -> ReconcileResolveByIdentity.
func seedFenceCrossedRun(t *testing.T, reg registry.Registry, runID string, runStatus spec.RunStatus, nodeStatus spec.NodeStatus, cancelIntent bool) string {
	t.Helper()
	specInput := spec.ExecutableRunSpec{
		Run:   spec.RunMetadata{RunID: runID, SubmittedAt: time.Now().UTC(), FailurePolicy: spec.FailurePolicy{Mode: "fail-fast"}},
		Graph: spec.Graph{Nodes: []spec.Node{{NodeID: "a", Image: "busybox:1.36"}}},
	}
	record := spec.RunRecord{RunID: runID, Status: runStatus, AcceptedAt: time.Now().UTC(), Spec: specInput}
	if runStatus == spec.RunStatusCanceled {
		record.TerminalStopCause = "canceled"
		record.TerminalFailureReason = "cancellation_requested"
	}
	attemptID := runID + "-a-attempt-1"
	nodes := []spec.NodeRecord{{RunID: runID, NodeID: "a", Status: nodeStatus, AttemptCount: 1, CurrentAttemptID: attemptID}}
	if err := reg.CreateRun(context.Background(), record, nodes); err != nil {
		t.Fatalf("CreateRun: %v", err)
	}
	if err := reg.UpsertAttempt(context.Background(), spec.AttemptRecord{RunID: runID, NodeID: "a", AttemptID: attemptID, Status: spec.AttemptStatusPrepared}); err != nil {
		t.Fatalf("UpsertAttempt: %v", err)
	}
	if err := reg.PersistSubmissionFence(context.Background(), runID, "a", attemptID, time.Now().UTC()); err != nil {
		t.Fatalf("PersistSubmissionFence: %v", err)
	}
	if cancelIntent {
		if err := reg.PersistCancellationIntent(context.Background(), runID, "a", attemptID, time.Now().UTC(), "cancellation_requested"); err != nil {
			t.Fatalf("PersistCancellationIntent: %v", err)
		}
	}
	return attemptID
}

// B1-T01: fence + crash + matching Job -> find, persist handle, reattach; Create=0.
func TestB1T01_FoundMatchingJobReattaches(t *testing.T) {
	reg := registry.NewMemoryRegistry()
	inner := &fakeAdapter{failOn: map[string]bool{}}
	adapter := newResolverAdapter(inner, backend.ResolveFound, fakeHandle{nodeID: "a"}, nil)
	engine := NewDagEngine(reg, adapter)

	runID := "run-b1-t01"
	seedFenceCrossedRun(t, reg, runID, spec.RunStatusRunning, spec.NodeStatusStarting, false)

	record, _ := reg.GetRun(context.Background(), runID)
	if err := engine.Admit(context.Background(), record); err != nil {
		t.Fatalf("Admit: %v", err)
	}
	waitForRunStatus(t, reg, runID, spec.RunStatusSucceeded)

	assertEventTypePresent(t, reg, runID, "node.recovery.identity_resolved")
	assertEventTypePresent(t, reg, runID, "node.recovery.reattached")
	if got := adapter.creates(); got != 0 {
		t.Fatalf("Create count = %d during recovery, want 0", got)
	}
	node, _ := reg.GetNode(context.Background(), runID, "a")
	if node.Status != spec.NodeStatusSucceeded {
		t.Fatalf("node status = %q, want Succeeded", node.Status)
	}
	if node.AttemptCount != 1 {
		t.Fatalf("AttemptCount = %d, want 1 (no replacement attempt)", node.AttemptCount)
	}
}

// B1-T02: fence + crash + matching Job + durable cancel intent (Canceled run
// recovery, driveCancellation path) -> find, persist, cancel same Job, observe
// terminal; Create=0.
func TestB1T02_FoundWithCancelIntentCancelsSameJob(t *testing.T) {
	reg := registry.NewMemoryRegistry()
	inner := &fakeAdapter{failOn: map[string]bool{"a": true}}
	adapter := newResolverAdapter(inner, backend.ResolveFound, fakeHandle{nodeID: "a"}, nil)
	engine := NewDagEngine(reg, adapter)

	runID := "run-b1-t02"
	seedFenceCrossedRun(t, reg, runID, spec.RunStatusCanceled, spec.NodeStatusRunning, true)

	if err := engine.Recover(context.Background()); err != nil {
		t.Fatalf("Recover: %v", err)
	}
	waitForNodeStatus(t, reg, runID, "a", spec.NodeStatusCanceled)

	if got := adapter.creates(); got != 0 {
		t.Fatalf("Create count = %d during cancel recovery, want 0", got)
	}
	inner.mu.Lock()
	canceled := inner.canceled["a"]
	inner.mu.Unlock()
	if !canceled {
		t.Fatal("expected CancelNode on the read-only-resolved same Job")
	}
	assertEventTypePresent(t, reg, runID, "node.recovery.identity_resolved")
	run, _ := reg.GetRun(context.Background(), runID)
	if run.Status != spec.RunStatusCanceled {
		t.Fatalf("run status = %q, want Canceled (recovery must not un-terminalize)", run.Status)
	}
	node, _ := reg.GetNode(context.Background(), runID, "a")
	if node.AttemptCount != 1 {
		t.Fatalf("AttemptCount = %d, want 1 (no replacement attempt)", node.AttemptCount)
	}
}

// B1-T03: backend NotFound -> ABSENT_NOW; no Create, no replacement, Run recoverable.
func TestB1T03_AbsentNowLeavesRecoverable(t *testing.T) {
	reg := registry.NewMemoryRegistry()
	inner := &fakeAdapter{failOn: map[string]bool{}}
	adapter := newResolverAdapter(inner, backend.ResolveAbsentNow, nil, nil)
	engine := NewDagEngine(reg, adapter)

	runID := "run-b1-t03"
	seedFenceCrossedRun(t, reg, runID, spec.RunStatusRunning, spec.NodeStatusStarting, false)

	record, _ := reg.GetRun(context.Background(), runID)
	if err := engine.Admit(context.Background(), record); err != nil {
		t.Fatalf("Admit: %v", err)
	}
	waitForEventType(t, reg, runID, "run.recovery.unresolved", 3*time.Second)
	time.Sleep(80 * time.Millisecond)

	if got := adapter.creates(); got != 0 {
		t.Fatalf("Create count = %d, want 0 for ABSENT_NOW", got)
	}
	assertEventTypePresent(t, reg, runID, "node.recovery.unresolved")
	run, _ := reg.GetRun(context.Background(), runID)
	if run.Status.IsTerminal() {
		t.Fatalf("run status = %q, want non-terminal (recoverable)", run.Status)
	}
	node, _ := reg.GetNode(context.Background(), runID, "a")
	if node.AttemptCount != 1 {
		t.Fatalf("AttemptCount = %d, want 1 (no replacement)", node.AttemptCount)
	}
	if node.Status.IsTerminal() {
		t.Fatalf("node status = %q, want non-terminal", node.Status)
	}
}

// B1-T04: annotation marker mismatch -> CONFLICT; no attach/create/replacement;
// distinct operator-visible conflict event.
func TestB1T04_ConflictEmitsEventAndDoesNotAttach(t *testing.T) {
	reg := registry.NewMemoryRegistry()
	inner := &fakeAdapter{failOn: map[string]bool{}}
	adapter := newResolverAdapter(inner, backend.ResolveConflict, nil, nil)
	engine := NewDagEngine(reg, adapter)

	runID := "run-b1-t04"
	seedFenceCrossedRun(t, reg, runID, spec.RunStatusRunning, spec.NodeStatusStarting, false)

	record, _ := reg.GetRun(context.Background(), runID)
	if err := engine.Admit(context.Background(), record); err != nil {
		t.Fatalf("Admit: %v", err)
	}
	waitForEventType(t, reg, runID, "node.recovery.identity_conflict", 3*time.Second)
	time.Sleep(80 * time.Millisecond)

	if got := adapter.creates(); got != 0 {
		t.Fatalf("Create count = %d, want 0 on CONFLICT", got)
	}
	assertEventPresent(t, reg, runID, "node.recovery.identity_conflict", "identity_conflict")
	assertEventTypePresent(t, reg, runID, "node.recovery.unresolved")
	assertEventAbsent(t, reg, runID, "node.recovery.reattached")
	run, _ := reg.GetRun(context.Background(), runID)
	if run.Status.IsTerminal() {
		t.Fatalf("run status = %q, want non-terminal (unresolved, not attached)", run.Status)
	}
	node, _ := reg.GetNode(context.Background(), runID, "a")
	if node.AttemptCount != 1 {
		t.Fatalf("AttemptCount = %d, want 1 (no replacement on CONFLICT)", node.AttemptCount)
	}
}

// B1-T05: Get timeout/forbidden -> UNKNOWN; unresolved, fail-closed.
func TestB1T05_UnknownFailsClosed(t *testing.T) {
	reg := registry.NewMemoryRegistry()
	inner := &fakeAdapter{failOn: map[string]bool{}}
	adapter := newResolverAdapter(inner, backend.ResolveUnknown, nil, errors.New("forbidden"))
	engine := NewDagEngine(reg, adapter)

	runID := "run-b1-t05"
	seedFenceCrossedRun(t, reg, runID, spec.RunStatusRunning, spec.NodeStatusStarting, false)

	record, _ := reg.GetRun(context.Background(), runID)
	if err := engine.Admit(context.Background(), record); err != nil {
		t.Fatalf("Admit: %v", err)
	}
	waitForEventType(t, reg, runID, "run.recovery.unresolved", 3*time.Second)
	time.Sleep(80 * time.Millisecond)

	if got := adapter.creates(); got != 0 {
		t.Fatalf("Create count = %d, want 0 on UNKNOWN", got)
	}
	assertEventTypePresent(t, reg, runID, "node.recovery.identity_unknown")
	assertEventTypePresent(t, reg, runID, "node.recovery.unresolved")
	run, _ := reg.GetRun(context.Background(), runID)
	if run.Status.IsTerminal() {
		t.Fatalf("run status = %q, want non-terminal (fail-closed recoverable)", run.Status)
	}
}

// B1-T06: repeated restart/resolution of a matching Job -> same handle/Attempt;
// no duplicate Job. After the first resolve persists a handle, a subsequent
// restart classifies as Reattach (no re-resolve, no create).
func TestB1T06_RepeatedResolutionIsIdempotent(t *testing.T) {
	reg := registry.NewMemoryRegistry()
	waitCh := make(chan struct{})
	inner := &fakeAdapter{failOn: map[string]bool{}, waitCh: map[string]chan struct{}{"a": waitCh}}
	adapter := newResolverAdapter(inner, backend.ResolveFound, fakeHandle{nodeID: "a"}, nil)
	engine := NewDagEngine(reg, adapter)

	runID := "run-b1-t06"
	seedFenceCrossedRun(t, reg, runID, spec.RunStatusRunning, spec.NodeStatusStarting, false)

	record, _ := reg.GetRun(context.Background(), runID)
	if err := engine.Admit(context.Background(), record); err != nil {
		t.Fatalf("Admit: %v", err)
	}
	// First resolve must persist the handle (blocked in WaitNode on waitCh).
	waitForEventType(t, reg, runID, "node.recovery.identity_resolved", 3*time.Second)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		n, _ := reg.GetNode(context.Background(), runID, "a")
		if n.CurrentAttemptHandleJSON != "" {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	// A subsequent restart now sees a durable handle: it must reattach, not
	// re-resolve or create.
	node, _ := reg.GetNode(context.Background(), runID, "a")
	att, hasAtt, _ := reg.GetCurrentAttempt(context.Background(), runID, "a")
	if got := ClassifyReconcile(node, att, hasAtt); got != ReconcileReattach {
		t.Fatalf("second restart decision = %v, want reattach (same handle/attempt)", got)
	}
	if got := adapter.resolves(); got != 1 {
		t.Fatalf("ResolveByIdentity calls = %d, want exactly 1 (idempotent)", got)
	}

	close(waitCh)
	waitForRunStatus(t, reg, runID, spec.RunStatusSucceeded)
	if got := adapter.creates(); got != 0 {
		t.Fatalf("Create count = %d, want 0 (no duplicate Job)", got)
	}
	final, _ := reg.GetNode(context.Background(), runID, "a")
	if final.AttemptCount != 1 {
		t.Fatalf("AttemptCount = %d, want 1 (no duplicate attempt)", final.AttemptCount)
	}
}

// B1-T07: found Job already terminal (failed) -> repair the same Attempt's
// terminal truth; no replay (no Create, no replacement Attempt).
func TestB1T07_FoundAlreadyTerminalNoReplay(t *testing.T) {
	reg := registry.NewMemoryRegistry()
	// The reattached Job is already terminal with an authoritative backend-reported
	// failure result (not a WaitNode observation loss); MaxAttempts default => no
	// retry/replacement, and the same attempt is repaired to that terminal truth.
	inner := &fakeAdapter{failOn: map[string]bool{}, waitResults: map[string]backend.ExecutionResult{"a": {Succeeded: false, TerminalStopCause: "failed", TerminalFailureReason: "user_code_failed"}}}
	adapter := newResolverAdapter(inner, backend.ResolveFound, fakeHandle{nodeID: "a"}, nil)
	engine := NewDagEngine(reg, adapter)

	runID := "run-b1-t07"
	seedFenceCrossedRun(t, reg, runID, spec.RunStatusRunning, spec.NodeStatusStarting, false)

	record, _ := reg.GetRun(context.Background(), runID)
	if err := engine.Admit(context.Background(), record); err != nil {
		t.Fatalf("Admit: %v", err)
	}
	waitForNodeStatus(t, reg, runID, "a", spec.NodeStatusFailed)

	if got := adapter.creates(); got != 0 {
		t.Fatalf("Create count = %d, want 0 (no replay of a terminal Job)", got)
	}
	assertEventTypePresent(t, reg, runID, "node.recovery.identity_resolved")
	node, _ := reg.GetNode(context.Background(), runID, "a")
	if node.AttemptCount != 1 {
		t.Fatalf("AttemptCount = %d, want 1 (repaired same attempt, no replay)", node.AttemptCount)
	}
}

// cancelFailingResolverAdapter is a resolverAdapter whose CancelNode always
// fails, modeling a transient API error / missing delete permission during
// recovery. It shadows the promoted (always-succeeding) fakeAdapter.CancelNode.
type cancelFailingResolverAdapter struct {
	*resolverAdapter
	cancelErr error
}

func (a *cancelFailingResolverAdapter) CancelNode(_ context.Context, _ backend.Handle) error {
	return a.cancelErr
}

// B1-FT1 (F-T1): FOUND + durable cancel intent, but CancelNode fails. Recovery
// must stay UNRESOLVED — never observe-on-canceled-ctx (which would read the ctx
// cancellation as CONFIRMED cancellation and terminalize a possibly-live Job),
// never reattach, never replace — so a later restart retries.
func TestB1FT1_FailedCancelDuringRecoveryStaysUnresolved(t *testing.T) {
	reg := registry.NewMemoryRegistry()
	inner := &fakeAdapter{failOn: map[string]bool{}}
	base := newResolverAdapter(inner, backend.ResolveFound, fakeHandle{nodeID: "a"}, nil)
	adapter := &cancelFailingResolverAdapter{resolverAdapter: base, cancelErr: errors.New("forbidden: cannot delete job")}
	engine := NewDagEngine(reg, adapter)

	runID := "run-b1-ft1"
	seedFenceCrossedRun(t, reg, runID, spec.RunStatusCanceled, spec.NodeStatusRunning, true)

	if err := engine.Recover(context.Background()); err != nil {
		t.Fatalf("Recover: %v", err)
	}
	waitForEventType(t, reg, runID, "node.recovery.unresolved", 3*time.Second)
	time.Sleep(80 * time.Millisecond)

	if got := adapter.creates(); got != 0 {
		t.Fatalf("Create count = %d, want 0 (failed cancel must not replace)", got)
	}
	assertEventTypePresent(t, reg, runID, "node.recovery.unresolved")
	assertEventAbsent(t, reg, runID, "node.recovery.reattached")
	node, _ := reg.GetNode(context.Background(), runID, "a")
	if node.Status.IsTerminal() {
		t.Fatalf("node status = %q, want non-terminal (failed cancel must not terminalize)", node.Status)
	}
	if node.AttemptCount != 1 {
		t.Fatalf("AttemptCount = %d, want 1 (no replacement)", node.AttemptCount)
	}
	run, _ := reg.GetRun(context.Background(), runID)
	if run.Status != spec.RunStatusCanceled {
		t.Fatalf("run status = %q, want Canceled (recovery must not un-terminalize)", run.Status)
	}
}

// persistFailRegistry is a registry whose PersistBackendHandle always fails,
// modeling a transient store error while durably persisting a resolved handle.
type persistFailRegistry struct {
	registry.Registry
	err error
}

func (r *persistFailRegistry) PersistBackendHandle(_ context.Context, _, _, _, _ string) error {
	return r.err
}

// B1-FT2 (F-T2): FOUND but the durable PersistBackendHandle fails. Recovery must
// NOT reattach on the in-memory-only handle (a crash mid-wait could lose it and,
// after TTL, resolve ABSENT_NOW forever). It must stay UNRESOLVED (non-terminal,
// recoverable) so the next restart re-resolves and re-persists.
func TestB1FT2_PersistFailureDoesNotReattach(t *testing.T) {
	mem := registry.NewMemoryRegistry()
	reg := &persistFailRegistry{Registry: mem, err: errors.New("sqlite: database is locked")}
	inner := &fakeAdapter{failOn: map[string]bool{}}
	adapter := newResolverAdapter(inner, backend.ResolveFound, fakeHandle{nodeID: "a"}, nil)
	engine := NewDagEngine(reg, adapter)

	runID := "run-b1-ft2"
	seedFenceCrossedRun(t, reg, runID, spec.RunStatusRunning, spec.NodeStatusStarting, false)

	record, _ := reg.GetRun(context.Background(), runID)
	if err := engine.Admit(context.Background(), record); err != nil {
		t.Fatalf("Admit: %v", err)
	}
	waitForEventType(t, reg, runID, "run.recovery.unresolved", 3*time.Second)
	time.Sleep(80 * time.Millisecond)

	if got := adapter.creates(); got != 0 {
		t.Fatalf("Create count = %d, want 0 (persist failure must not replace)", got)
	}
	assertEventTypePresent(t, reg, runID, "node.recovery.unresolved")
	assertEventAbsent(t, reg, runID, "node.recovery.identity_resolved")
	assertEventAbsent(t, reg, runID, "node.recovery.reattached")
	run, _ := reg.GetRun(context.Background(), runID)
	if run.Status.IsTerminal() {
		t.Fatalf("run status = %q, want non-terminal (recoverable)", run.Status)
	}
	node, _ := reg.GetNode(context.Background(), runID, "a")
	if node.Status.IsTerminal() {
		t.Fatalf("node status = %q, want non-terminal", node.Status)
	}
	if node.CurrentAttemptHandleJSON != "" {
		t.Fatalf("handle JSON = %q, want empty (persist failed, nothing durable)", node.CurrentAttemptHandleJSON)
	}
}

// B1-T08: stale node projection but the Attempt-scoped durable facts require a
// resolve -> the Attempt facts win (resolve fires and reattaches), not the node
// projection. The node claims Running with no handle, yet the Attempt's fence
// fact drives ResolveByIdentity.
func TestB1T08_AttemptFactsWinOverStaleNodeProjection(t *testing.T) {
	reg := registry.NewMemoryRegistry()
	inner := &fakeAdapter{failOn: map[string]bool{}}
	adapter := newResolverAdapter(inner, backend.ResolveFound, fakeHandle{nodeID: "a"}, nil)
	engine := NewDagEngine(reg, adapter)

	runID := "run-b1-t08"
	// Node projection stale: claims Running, but carries no handle. The Attempt is
	// the authority (fence crossed, no handle).
	seedFenceCrossedRun(t, reg, runID, spec.RunStatusRunning, spec.NodeStatusRunning, false)

	// Confirm the classification is driven by the Attempt fact, not the node.
	node, _ := reg.GetNode(context.Background(), runID, "a")
	att, hasAtt, _ := reg.GetCurrentAttempt(context.Background(), runID, "a")
	if got := ClassifyReconcile(node, att, hasAtt); got != ReconcileResolveByIdentity {
		t.Fatalf("classify = %v, want resolve_by_identity (Attempt fence fact wins)", got)
	}

	record, _ := reg.GetRun(context.Background(), runID)
	if err := engine.Admit(context.Background(), record); err != nil {
		t.Fatalf("Admit: %v", err)
	}
	waitForRunStatus(t, reg, runID, spec.RunStatusSucceeded)

	if got := adapter.creates(); got != 0 {
		t.Fatalf("Create count = %d, want 0", got)
	}
	if got := adapter.resolves(); got < 1 {
		t.Fatalf("ResolveByIdentity calls = %d, want >=1 (Attempt facts drove resolve)", got)
	}
	assertEventTypePresent(t, reg, runID, "node.recovery.identity_resolved")
}
