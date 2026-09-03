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

// F3-B3 (semantic Attempt opening & realization accounting).
//
// Invariants: RetryPolicy.MaxAttempts is the user-authored execution-opportunity
// budget ONLY; pre-user-code realization has a SEPARATE finite budget whose ceiling
// is internal and independent of MaxAttempts; a semantic Attempt (and its user-code
// opportunity) opens only at the submission fence; a bare restart of a pre-open
// realization cycle spends no new budget; B1/B2 safety boundaries are preserved.

// fenceFailingRegistry forces OpenSemanticAttempt (the fence/open transition) to fail
// for a node, so tests can prove that a failed fence opens no backend workload and
// consumes no user-code opportunity.
type fenceFailingRegistry struct {
	registry.Registry
	nodeID string
}

func (f *fenceFailingRegistry) OpenSemanticAttempt(ctx context.Context, runID, nodeID, attemptID string, openedAt time.Time) error {
	if nodeID == f.nodeID {
		return fmt.Errorf("forced fence-open failure")
	}
	return f.Registry.OpenSemanticAttempt(ctx, runID, nodeID, attemptID, openedAt)
}

// seedPreFenceReservation seeds a node holding a non-semantic pre-fence reservation:
// a Prepared current attempt with no submission fence and no backend handle, plus a
// spent realization cycle. ClassifyReconcile routes this to ReconcileResumePreFence.
func seedPreFenceReservation(t *testing.T, reg registry.Registry, runID string) string {
	t.Helper()
	specInput := spec.ExecutableRunSpec{
		Run:   spec.RunMetadata{RunID: runID, SubmittedAt: time.Now().UTC(), FailurePolicy: spec.FailurePolicy{Mode: "fail-fast"}},
		Graph: spec.Graph{Nodes: []spec.Node{{NodeID: "a", Image: "busybox:1.36"}}},
	}
	record := spec.RunRecord{RunID: runID, Status: spec.RunStatusRunning, AcceptedAt: time.Now().UTC(), Spec: specInput}
	attemptID := spec.DeterministicAttemptID(runID, "a", 1)
	nodes := []spec.NodeRecord{{
		RunID: runID, NodeID: "a", Status: spec.NodeStatusReady,
		RealizationAttemptCount: 1, CurrentAttemptID: attemptID, CurrentBottleneckLocation: "release_wait",
	}}
	if err := reg.CreateRun(context.Background(), record, nodes); err != nil {
		t.Fatalf("CreateRun: %v", err)
	}
	now := time.Now().UTC()
	if err := reg.UpsertAttempt(context.Background(), spec.AttemptRecord{
		RunID: runID, NodeID: "a", AttemptID: attemptID, Status: spec.AttemptStatusPrepared, StartedAt: &now,
	}); err != nil {
		t.Fatalf("UpsertAttempt: %v", err)
	}
	return attemptID
}

// B3-V1: MaxAttempts=1; pre-submission backend_prepare_error; no fence/Job -> must
// not consume the user-code execution-opportunity budget.
func TestB3V1_PreSubmissionErrorDoesNotConsumeOpportunity(t *testing.T) {
	reg := registry.NewMemoryRegistry()
	withRealizationCeiling(t, 1)
	adapter := &fakeAdapter{failOn: map[string]bool{}, failPrepareOn: map[string]bool{"a": true}}
	engine := NewDagEngine(reg, adapter)

	runID := "b3-v1"
	b2Run(t, engine, reg, runID, spec.Node{NodeID: "a", Image: "busybox:1.36", RetryPolicy: spec.RetryPolicy{MaxAttempts: 1}})
	waitForRunStatusWithin(t, reg, runID, spec.RunStatusFailed, 3*time.Second)

	node, _ := reg.GetNode(context.Background(), runID, "a")
	if node.AttemptCount != 0 {
		t.Fatalf("AttemptCount = %d, want 0 (a pre-submission failure must not consume the MaxAttempts opportunity budget)", node.AttemptCount)
	}
	if got := adapter.startCount("a"); got != 0 {
		t.Fatalf("StartNode calls = %d, want 0 (no workload opened)", got)
	}
	// No semantic Attempt opened: no attempt carries a submission fence.
	attempts, _ := reg.ListAttempts(context.Background(), runID, "a")
	for _, a := range attempts {
		if a.SubmissionWindowOpenedAt != nil {
			t.Fatalf("attempt %s crossed the fence; no semantic Attempt should have opened", a.AttemptID)
		}
	}
}

// B3-V2: repeated replay-safe pre-submission E0 failures are bounded by the
// realization budget; no infinite loop, and the ceiling is independent of MaxAttempts.
func TestB3V2_RealizationBudgetIsBoundedAndIndependent(t *testing.T) {
	reg := registry.NewMemoryRegistry()
	withRealizationCeiling(t, 3)
	// MaxAttempts far larger than the realization ceiling: boundedness must come from
	// the INDEPENDENT realization ceiling, not from MaxAttempts.
	adapter := &fakeAdapter{failOn: map[string]bool{}, failPrepareOn: map[string]bool{"a": true}}
	engine := NewDagEngine(reg, adapter)

	runID := "b3-v2"
	b2Run(t, engine, reg, runID, spec.Node{NodeID: "a", Image: "busybox:1.36", RetryPolicy: spec.RetryPolicy{MaxAttempts: 99}})
	waitForRunStatusWithin(t, reg, runID, spec.RunStatusFailed, 3*time.Second)

	node, _ := reg.GetNode(context.Background(), runID, "a")
	if node.RealizationAttemptCount != 3 {
		t.Fatalf("RealizationAttemptCount = %d, want 3 (bounded by the independent ceiling, not MaxAttempts=99)", node.RealizationAttemptCount)
	}
	if node.AttemptCount != 0 {
		t.Fatalf("AttemptCount = %d, want 0", node.AttemptCount)
	}
	if node.Status != spec.NodeStatusFailed {
		t.Fatalf("node status = %q, want Failed (exhausted realization budget fails closed)", node.Status)
	}
}

// B3-V3: submission-fence persistence fails -> no semantic Attempt opens and no
// backend Create happens.
func TestB3V3_FenceOpenFailureOpensNoWorkload(t *testing.T) {
	base := registry.NewMemoryRegistry()
	reg := &fenceFailingRegistry{Registry: base, nodeID: "a"}
	withRealizationCeiling(t, 1)
	adapter := &fakeAdapter{failOn: map[string]bool{}}
	engine := NewDagEngine(reg, adapter)

	runID := "b3-v3"
	b2Run(t, engine, reg, runID, spec.Node{NodeID: "a", Image: "busybox:1.36", RetryPolicy: spec.RetryPolicy{MaxAttempts: 1}})
	waitForRunStatusWithin(t, reg, runID, spec.RunStatusFailed, 3*time.Second)

	if got := adapter.startCount("a"); got != 0 {
		t.Fatalf("StartNode calls = %d, want 0 (a failed fence must not open a backend workload)", got)
	}
	node, _ := reg.GetNode(context.Background(), runID, "a")
	if node.AttemptCount != 0 {
		t.Fatalf("AttemptCount = %d, want 0 (no semantic Attempt opened)", node.AttemptCount)
	}
	attempts, _ := reg.ListAttempts(context.Background(), runID, "a")
	for _, a := range attempts {
		if a.SubmissionWindowOpenedAt != nil {
			t.Fatalf("attempt %s crossed the fence despite fence-open failure", a.AttemptID)
		}
	}
}

// B3-V4 / B3-V9: crash after a reservation but before the semantic Attempt opens ->
// restart reuses/reconciles the SAME reservation; it does not fabricate a phantom
// Attempt and does not spend an extra realization budget unit merely for restarting.
func TestB3V4_RestartReusesReservationNoExtraBudget(t *testing.T) {
	reg := registry.NewMemoryRegistry()
	adapter := &persistableAdapter{fakeAdapter: &fakeAdapter{failOn: map[string]bool{}}}
	engine := NewDagEngine(reg, adapter)

	runID := "b3-v4"
	seedPreFenceReservation(t, reg, runID)
	if err := engine.Recover(context.Background()); err != nil {
		t.Fatalf("Recover: %v", err)
	}
	waitForRunStatusWithin(t, reg, runID, spec.RunStatusSucceeded, 3*time.Second)

	node, _ := reg.GetNode(context.Background(), runID, "a")
	if node.RealizationAttemptCount != 1 {
		t.Fatalf("RealizationAttemptCount = %d, want 1 (restart resumed the reservation; no extra realization unit spent)", node.RealizationAttemptCount)
	}
	if node.AttemptCount != 1 {
		t.Fatalf("AttemptCount = %d, want 1 (the semantic Attempt opened once at the fence on resume)", node.AttemptCount)
	}
	if got := adapter.startCount("a"); got != 1 {
		t.Fatalf("StartNode calls = %d, want 1 (one workload, no phantom attempt)", got)
	}
	attempts, _ := reg.ListAttempts(context.Background(), runID, "a")
	if len(attempts) != 1 {
		t.Fatalf("attempt rows = %d, want 1 (reservation reused, not re-allocated)", len(attempts))
	}
}

// B3-V5 / B3-V7 / B3-V8: an opened Job's post-submission failure is may-have-started
// (E3) unless authoritative E0 is proven; the realization budget must NOT authorize a
// generic replacement execution after the fence has opened. (Authoritative E0 after
// open — the refund/new-workload path — has no producing code path today; the
// reachable invariant is that no post-open replacement occurs.)
func TestB3V5_RealizationBudgetDoesNotReplacePostOpen(t *testing.T) {
	reg := registry.NewMemoryRegistry()
	withRealizationCeiling(t, 5)
	adapter := &fakeAdapter{
		failOn:      map[string]bool{},
		waitResults: map[string]backend.ExecutionResult{"a": {Succeeded: false, TerminalStopCause: "failed", TerminalFailureReason: "user_code_failed"}},
	}
	engine := NewDagEngine(reg, adapter)

	runID := "b3-v5"
	b2Run(t, engine, reg, runID, spec.Node{NodeID: "a", Image: "busybox:1.36", RetryPolicy: spec.RetryPolicy{MaxAttempts: 3}})
	waitForRunStatusWithin(t, reg, runID, spec.RunStatusFailed, 3*time.Second)

	node, _ := reg.GetNode(context.Background(), runID, "a")
	if node.AttemptCount != 1 {
		t.Fatalf("AttemptCount = %d, want 1 (one opportunity consumed at the fence)", node.AttemptCount)
	}
	if got := adapter.startCount("a"); got != 1 {
		t.Fatalf("StartNode calls = %d, want 1 (post-open failure must not be replaced from the realization budget)", got)
	}
	// The realization budget was spent once (the single realization cycle that opened
	// the Attempt); it did NOT spin up further post-open workloads.
	if node.RealizationAttemptCount != 1 {
		t.Fatalf("RealizationAttemptCount = %d, want 1 (no post-open realization replacement)", node.RealizationAttemptCount)
	}
}

// B3-V6: an opened, fence-crossed Attempt whose backend truth is ABSENT_NOW routes to
// B1 reconcile-first; the realization budget cannot authorize a replacement Job.
func TestB3V6_AbsentNowReconcilesNoRealizationReplacement(t *testing.T) {
	reg := registry.NewMemoryRegistry()
	withRealizationCeiling(t, 5)
	inner := &fakeAdapter{failOn: map[string]bool{}}
	adapter := newResolverAdapter(inner, backend.ResolveAbsentNow, nil, nil)
	engine := NewDagEngine(reg, adapter)

	runID := "b3-v6"
	seedFenceCrossedRun(t, reg, runID, spec.RunStatusRunning, spec.NodeStatusStarting, false)
	record, _ := reg.GetRun(context.Background(), runID)
	if err := engine.Admit(context.Background(), record); err != nil {
		t.Fatalf("Admit: %v", err)
	}
	waitForEventType(t, reg, runID, "node.recovery.unresolved", 3*time.Second)

	if got := adapter.creates(); got != 0 {
		t.Fatalf("Create count = %d, want 0 (ABSENT_NOW must not be replaced from the realization budget)", got)
	}
	node, _ := reg.GetNode(context.Background(), runID, "a")
	if node.Status.IsTerminal() {
		t.Fatalf("node terminalized on ABSENT_NOW: %s (must stay recoverable)", node.Status)
	}
}

// B3-V10: MaxAttempts=2; realization retries before the fence do not consume the
// user-code opportunity budget, and the single successful execution consumes exactly
// one opportunity.
func TestB3V10_RealizationRetriesDoNotSpendOpportunity(t *testing.T) {
	reg := registry.NewMemoryRegistry()
	withRealizationCeiling(t, 5)
	// Prepare fails once (one realization retry), then succeeds and the workload runs.
	adapter := &fakeAdapter{failOn: map[string]bool{}, failPrepareTimes: map[string]int{"a": 1}}
	engine := NewDagEngine(reg, adapter)

	runID := "b3-v10"
	b2Run(t, engine, reg, runID, spec.Node{NodeID: "a", Image: "busybox:1.36", RetryPolicy: spec.RetryPolicy{MaxAttempts: 2}})
	waitForRunStatusWithin(t, reg, runID, spec.RunStatusSucceeded, 3*time.Second)

	node, _ := reg.GetNode(context.Background(), runID, "a")
	if node.RealizationAttemptCount != 2 {
		t.Fatalf("RealizationAttemptCount = %d, want 2 (one failed + one successful realization cycle)", node.RealizationAttemptCount)
	}
	if node.AttemptCount != 1 {
		t.Fatalf("AttemptCount = %d, want 1 (exactly one user-code opportunity consumed; the earlier realization retry consumed none)", node.AttemptCount)
	}
	if got := adapter.startCount("a"); got != 1 {
		t.Fatalf("StartNode calls = %d, want 1", got)
	}
}

// B3-V11: MaxAttempts>1 alone never turns a post-open (E3) failure into a retry.
func TestB3V11_MaxAttemptsAloneNeverReexecutesPostOpen(t *testing.T) {
	reg := registry.NewMemoryRegistry()
	withRealizationCeiling(t, 9)
	adapter := &fakeAdapter{failOn: map[string]bool{"a": true}} // WaitNode observation loss
	engine := NewDagEngine(reg, adapter)

	runID := "b3-v11"
	b2Run(t, engine, reg, runID, spec.Node{NodeID: "a", Image: "busybox:1.36", RetryPolicy: spec.RetryPolicy{MaxAttempts: 9}})
	waitForEventType(t, reg, runID, "node.recovery.unresolved", 3*time.Second)

	node, _ := reg.GetNode(context.Background(), runID, "a")
	if node.AttemptCount != 1 {
		t.Fatalf("AttemptCount = %d, want 1 (a large MaxAttempts must not open a second opportunity for a post-open failure)", node.AttemptCount)
	}
	if got := adapter.startCount("a"); got != 1 {
		t.Fatalf("StartNode calls = %d, want 1 (no re-execution)", got)
	}
}

// B3-V12: realization/candidate ids may leave gaps relative to the opportunity count;
// no consumer may treat numeric gaplessness as semantic execution evidence. Here two
// realization cycles precede a single opened opportunity, so the opened attempt id is
// derived from realization cycle 2 while AttemptCount is 1 (a deliberate gap).
func TestB3V12_IdGapsAreNotExecutionEvidence(t *testing.T) {
	reg := registry.NewMemoryRegistry()
	withRealizationCeiling(t, 5)
	adapter := &fakeAdapter{failOn: map[string]bool{}, failPrepareTimes: map[string]int{"a": 1}}
	engine := NewDagEngine(reg, adapter)

	runID := "b3-v12"
	b2Run(t, engine, reg, runID, spec.Node{NodeID: "a", Image: "busybox:1.36", RetryPolicy: spec.RetryPolicy{MaxAttempts: 2}})
	waitForRunStatusWithin(t, reg, runID, spec.RunStatusSucceeded, 3*time.Second)

	node, _ := reg.GetNode(context.Background(), runID, "a")
	// A gap exists: two realization cycles, one opportunity consumed.
	if node.RealizationAttemptCount == node.AttemptCount {
		t.Fatalf("expected a gap between RealizationAttemptCount(%d) and AttemptCount(%d)", node.RealizationAttemptCount, node.AttemptCount)
	}
	// The opened (successful) attempt id corresponds to realization cycle 2, not to the
	// opportunity count of 1 — the run still succeeds, i.e. no consumer required
	// gapless sequential ids as execution evidence.
	want := spec.DeterministicAttemptID(runID, "a", 2)
	if node.CurrentAttemptID != want {
		t.Fatalf("current attempt id = %q, want %q (id follows the realization cycle, gaps allowed)", node.CurrentAttemptID, want)
	}
	if node.Status != spec.NodeStatusSucceeded {
		t.Fatalf("node status = %q, want Succeeded", node.Status)
	}
}
