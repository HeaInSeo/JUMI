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

// F3-B2 (JUMI #46) — execution-evidence-gated re-execution safety.
//
// Invariant under test: RetryPolicy.MaxAttempts is a budget/cap, never standalone
// re-execution authority. A new user-code execution (a new semantic Attempt / Job)
// is admissible only for authoritative Q32 E0 (user code could not have started);
// E1/E3/E4 or unknown evidence must not turn remaining budget into a new user-code
// opportunity, and a post-success finalization failure must never rerun user code.

// b2Run admits a single-node run and returns its record.
func b2Run(t *testing.T, engine *DagEngine, reg registry.Registry, runID string, node spec.Node) spec.RunRecord {
	t.Helper()
	specInput := spec.ExecutableRunSpec{
		Run:   spec.RunMetadata{RunID: runID, SampleRunID: runID, SubmittedAt: time.Now().UTC(), FailurePolicy: spec.FailurePolicy{Mode: "fail-fast"}},
		Graph: spec.Graph{Nodes: []spec.Node{node}},
	}
	record := spec.RunRecord{RunID: runID, Status: spec.RunStatusAccepted, AcceptedAt: time.Now().UTC(), Spec: specInput}
	nodes := []spec.NodeRecord{{RunID: runID, NodeID: node.NodeID, Status: spec.NodeStatusPending}}
	if err := reg.CreateRun(context.Background(), record, nodes); err != nil {
		t.Fatalf("CreateRun: %v", err)
	}
	if err := engine.Admit(context.Background(), record); err != nil {
		t.Fatalf("Admit: %v", err)
	}
	return record
}

// B2-T01: process succeeds; register_artifact_error; MaxAttempts>1 -> user-code
// execution count remains 1; no new Attempt/Job solely due to registration failure.
func TestB2T01_RegisterErrorAfterSuccessNoNewExecution(t *testing.T) {
	reg := registry.NewMemoryRegistry()
	adapter := &fakeAdapter{failOn: map[string]bool{}}
	hc := &fakeHandoffClient{registerErr: fmt.Errorf("register down")}
	engine := NewDagEngineWithHandoff(reg, adapter, hc)

	runID := "b2-t01"
	b2Run(t, engine, reg, runID, spec.Node{NodeID: "a", Image: "busybox:1.36", Outputs: []string{"o.json"}, RetryPolicy: spec.RetryPolicy{MaxAttempts: 3}})
	waitForEventType(t, reg, runID, "node.finalization.deferred", 3*time.Second)

	node, _ := reg.GetNode(context.Background(), runID, "a")
	if node.AttemptCount != 1 {
		t.Fatalf("AttemptCount = %d, want 1 (MaxAttempts>1 must not open a new attempt for a finalization failure)", node.AttemptCount)
	}
	if got := adapter.startCount("a"); got != 1 {
		t.Fatalf("StartNode calls = %d, want 1 (user code must not be re-run)", got)
	}
	if node.Status.IsTerminal() {
		t.Fatalf("node terminalized on finalization failure: %s", node.Status)
	}
}

// B2-T02: process succeeds; notify_node_terminal_error; MaxAttempts>1 -> execution
// count remains 1.
func TestB2T02_NotifyErrorAfterSuccessNoNewExecution(t *testing.T) {
	reg := registry.NewMemoryRegistry()
	adapter := &fakeAdapter{failOn: map[string]bool{}}
	hc := &fakeHandoffClient{notifyErr: fmt.Errorf("notify down")}
	engine := NewDagEngineWithHandoff(reg, adapter, hc)

	runID := "b2-t02"
	b2Run(t, engine, reg, runID, spec.Node{NodeID: "a", Image: "busybox:1.36", RetryPolicy: spec.RetryPolicy{MaxAttempts: 3}})
	waitForEventType(t, reg, runID, "node.finalization.deferred", 3*time.Second)

	node, _ := reg.GetNode(context.Background(), runID, "a")
	if node.AttemptCount != 1 {
		t.Fatalf("AttemptCount = %d, want 1", node.AttemptCount)
	}
	if got := adapter.startCount("a"); got != 1 {
		t.Fatalf("StartNode calls = %d, want 1 (no user-code rerun)", got)
	}
}

// B2-T03: backend wait uncertainty (WaitNode observation loss) where user code may
// have started and no E0 proof exists -> no generic new Attempt, and the Attempt is
// NOT terminalized (it is left recoverable so reconcile reattaches the same Job);
// even with a remaining budget, no user-code rerun occurs.
func TestB2T03_BackendWaitUncertaintyNoNewAttempt(t *testing.T) {
	reg := registry.NewMemoryRegistry()
	adapter := &fakeAdapter{failOn: map[string]bool{"a": true}} // WaitNode returns an error (observation loss)
	engine := NewDagEngine(reg, adapter)

	runID := "b2-t03"
	b2Run(t, engine, reg, runID, spec.Node{NodeID: "a", Image: "busybox:1.36", RetryPolicy: spec.RetryPolicy{MaxAttempts: 3}})
	waitForEventType(t, reg, runID, "node.recovery.unresolved", 3*time.Second)

	node, _ := reg.GetNode(context.Background(), runID, "a")
	if node.Status.IsTerminal() {
		t.Fatalf("observation loss terminalized the node (%s); it must stay recoverable, not Failed", node.Status)
	}
	if node.AttemptCount != 1 {
		t.Fatalf("AttemptCount = %d, want 1 (may-have-started must not consume the retry budget)", node.AttemptCount)
	}
	if got := adapter.startCount("a"); got != 1 {
		t.Fatalf("StartNode calls = %d, want 1 (no user-code rerun on observation loss)", got)
	}
}

// B2-T04: backend reports failure but does not authoritatively prove no-start ->
// fail-safe may-have-started; no generic new execution for unknown effect safety.
func TestB2T04_BackendReportedFailureNoNewExecution(t *testing.T) {
	reg := registry.NewMemoryRegistry()
	adapter := &fakeAdapter{
		failOn:      map[string]bool{},
		waitResults: map[string]backend.ExecutionResult{"a": {Succeeded: false, TerminalStopCause: "failed", TerminalFailureReason: "user_code_failed"}},
	}
	engine := NewDagEngine(reg, adapter)

	runID := "b2-t04"
	b2Run(t, engine, reg, runID, spec.Node{NodeID: "a", Image: "busybox:1.36", RetryPolicy: spec.RetryPolicy{MaxAttempts: 3}})
	waitForRunStatusWithin(t, reg, runID, spec.RunStatusFailed, 3*time.Second)

	node, _ := reg.GetNode(context.Background(), runID, "a")
	if node.AttemptCount != 1 {
		t.Fatalf("AttemptCount = %d, want 1 (backend-reported failure is not E0; no new execution)", node.AttemptCount)
	}
	if got := adapter.startCount("a"); got != 1 {
		t.Fatalf("StartNode calls = %d, want 1", got)
	}
}

// B2-T05: authoritative E0 (pre-submission prepare failure) + bounded budget -> a
// re-realization attempt remains possible; the safety gate does not prohibit all
// retries, only ungated post-start re-execution.
func TestB2T05_PreStartE0RetryStillAllowed(t *testing.T) {
	reg := registry.NewMemoryRegistry()
	adapter := &fakeAdapter{failOn: map[string]bool{}, failPrepareOn: map[string]bool{"a": true}}
	engine := NewDagEngine(reg, adapter)

	runID := "b2-t05"
	b2Run(t, engine, reg, runID, spec.Node{NodeID: "a", Image: "busybox:1.36", RetryPolicy: spec.RetryPolicy{MaxAttempts: 2}})
	waitForRunStatusWithin(t, reg, runID, spec.RunStatusFailed, 3*time.Second)

	node, _ := reg.GetNode(context.Background(), runID, "a")
	if node.AttemptCount != 2 {
		t.Fatalf("AttemptCount = %d, want 2 (E0 pre-submission failures remain retry-eligible)", node.AttemptCount)
	}
	if got := adapter.startCount("a"); got != 0 {
		t.Fatalf("StartNode calls = %d, want 0 (prepare failed before the submission boundary; user code never started)", got)
	}
}

// B2-T06: B1 ABSENT_NOW is not E0 proof -> no new execution / no replacement Job.
func TestB2T06_AbsentNowIsNotE0(t *testing.T) {
	reg := registry.NewMemoryRegistry()
	inner := &fakeAdapter{failOn: map[string]bool{}}
	adapter := newResolverAdapter(inner, backend.ResolveAbsentNow, nil, nil)
	engine := NewDagEngine(reg, adapter)

	runID := "b2-t06"
	seedFenceCrossedRun(t, reg, runID, spec.RunStatusRunning, spec.NodeStatusStarting, false)
	record, _ := reg.GetRun(context.Background(), runID)
	if err := engine.Admit(context.Background(), record); err != nil {
		t.Fatalf("Admit: %v", err)
	}
	waitForEventType(t, reg, runID, "node.recovery.unresolved", 3*time.Second)

	if got := adapter.creates(); got != 0 {
		t.Fatalf("Create count = %d, want 0 (ABSENT_NOW must not create a replacement Job)", got)
	}
	node, _ := reg.GetNode(context.Background(), runID, "a")
	if node.AttemptCount != 1 {
		t.Fatalf("AttemptCount = %d, want 1 (no replacement attempt)", node.AttemptCount)
	}
	if node.Status.IsTerminal() {
		t.Fatalf("node terminalized on ABSENT_NOW: %s (must stay recoverable)", node.Status)
	}
}

// B2-T07: evidence classification unavailable/ambiguous (UNKNOWN) -> fail closed;
// no new execution.
func TestB2T07_UnknownEvidenceFailsClosed(t *testing.T) {
	reg := registry.NewMemoryRegistry()
	inner := &fakeAdapter{failOn: map[string]bool{}}
	adapter := newResolverAdapter(inner, backend.ResolveUnknown, nil, nil)
	engine := NewDagEngine(reg, adapter)

	runID := "b2-t07"
	seedFenceCrossedRun(t, reg, runID, spec.RunStatusRunning, spec.NodeStatusStarting, false)
	record, _ := reg.GetRun(context.Background(), runID)
	if err := engine.Admit(context.Background(), record); err != nil {
		t.Fatalf("Admit: %v", err)
	}
	waitForEventType(t, reg, runID, "node.recovery.unresolved", 3*time.Second)

	if got := adapter.creates(); got != 0 {
		t.Fatalf("Create count = %d, want 0 (UNKNOWN evidence must fail closed)", got)
	}
	node, _ := reg.GetNode(context.Background(), runID, "a")
	if node.AttemptCount != 1 {
		t.Fatalf("AttemptCount = %d, want 1", node.AttemptCount)
	}
}

// B2-T08: restart after process completion but before/during platform finalization
// -> finalization is re-driven/reconciled without rerunning user code.
func TestB2T08_RestartRedrivesFinalizationNoRerun(t *testing.T) {
	reg := registry.NewMemoryRegistry()
	inner := &fakeAdapter{failOn: map[string]bool{}}
	adapter := &persistableAdapter{fakeAdapter: inner}
	hc := &fakeHandoffClient{registerErr: fmt.Errorf("register down")}
	engine1 := NewDagEngineWithHandoff(reg, adapter, hc)

	runID := "b2-t08"
	b2Run(t, engine1, reg, runID, spec.Node{NodeID: "a", Image: "busybox:1.36", Outputs: []string{"o.json"}})
	waitForEventType(t, reg, runID, "node.finalization.deferred", 3*time.Second)

	node, _ := reg.GetNode(context.Background(), runID, "a")
	if node.CurrentAttemptHandleJSON == "" {
		t.Fatalf("expected a persisted backend handle for finalization reattach")
	}
	if node.Status.IsTerminal() {
		t.Fatalf("node terminalized before restart: %s", node.Status)
	}
	// The E4 success fact must be durably persisted (§4), so recovery never re-runs
	// user code even if the backend Job is later garbage-collected.
	if att, ok, _ := reg.GetCurrentAttempt(context.Background(), runID, "a"); !ok || att.ProcessCompletedAt == nil {
		t.Fatalf("expected durable ProcessCompletedAt on the deferred attempt (ok=%v)", ok)
	}
	if got := adapter.startCount("a"); got != 1 {
		t.Fatalf("StartNode calls before restart = %d, want 1", got)
	}

	// Simulate restart: finalization dependency recovers, a fresh engine reconciles.
	hc.mu.Lock()
	hc.registerErr = nil
	hc.mu.Unlock()
	engine2 := NewDagEngineWithHandoff(reg, adapter, hc)
	if err := engine2.Recover(context.Background()); err != nil {
		t.Fatalf("Recover: %v", err)
	}
	waitForRunStatusWithin(t, reg, runID, spec.RunStatusSucceeded, 3*time.Second)

	node, _ = reg.GetNode(context.Background(), runID, "a")
	if node.Status != spec.NodeStatusSucceeded {
		t.Fatalf("node status after restart = %q, want Succeeded", node.Status)
	}
	if got := adapter.startCount("a"); got != 1 {
		t.Fatalf("StartNode calls total = %d, want 1 (finalization re-driven by reattach, user code NOT re-run)", got)
	}
	if node.AttemptCount != 1 {
		t.Fatalf("AttemptCount = %d, want 1 (no new attempt for finalization reconcile)", node.AttemptCount)
	}
}

// B2-T08b: durable E4 evidence survives backend Job garbage-collection. A restart
// where the process had completed (ProcessCompletedAt persisted) but the Job is now
// gone (no handle; identity resolve -> ABSENT_NOW) must NEVER re-run user code and
// must not be terminalized by a replacement execution.
func TestB2T08b_ProcessCompletedSurvivesJobGCNoRerun(t *testing.T) {
	reg := registry.NewMemoryRegistry()
	inner := &fakeAdapter{failOn: map[string]bool{}}
	adapter := newResolverAdapter(inner, backend.ResolveAbsentNow, nil, nil)
	engine := NewDagEngine(reg, adapter)

	runID := "b2-t08b"
	// Seed: process completed (durable ProcessCompletedAt), fence crossed, but the
	// backend handle/Job is gone (GC'd past TTL) — the E4-deferred-then-GC state.
	attemptID := seedFenceCrossedRun(t, reg, runID, spec.RunStatusRunning, spec.NodeStatusStarting, false)
	if err := reg.PersistProcessCompleted(context.Background(), runID, "a", attemptID, time.Now().UTC()); err != nil {
		t.Fatalf("PersistProcessCompleted: %v", err)
	}

	record, _ := reg.GetRun(context.Background(), runID)
	if err := engine.Admit(context.Background(), record); err != nil {
		t.Fatalf("Admit: %v", err)
	}
	waitForEventType(t, reg, runID, "node.recovery.unresolved", 3*time.Second)

	if got := adapter.creates(); got != 0 {
		t.Fatalf("Create count = %d, want 0 (a GC'd Job after process success must not be re-executed)", got)
	}
	node, _ := reg.GetNode(context.Background(), runID, "a")
	if node.Status == spec.NodeStatusFailed {
		t.Fatalf("a successfully-executed run was marked Failed after Job GC; must stay recoverable")
	}
	if node.AttemptCount != 1 {
		t.Fatalf("AttemptCount = %d, want 1 (no replacement attempt)", node.AttemptCount)
	}
}

// B2-T09: a legitimately admitted new execution (an E0 retry) opens a NEW semantic
// Attempt; the prior Attempt is never reused to launch a second user-code
// opportunity (1 semantic Attempt : 1 Kubernetes Job).
func TestB2T09_NewExecutionUsesNewSemanticAttempt(t *testing.T) {
	reg := registry.NewMemoryRegistry()
	adapter := &fakeAdapter{failOn: map[string]bool{}, failPrepareOn: map[string]bool{"a": true}}
	engine := NewDagEngine(reg, adapter)

	runID := "b2-t09"
	b2Run(t, engine, reg, runID, spec.Node{NodeID: "a", Image: "busybox:1.36", RetryPolicy: spec.RetryPolicy{MaxAttempts: 2}})
	waitForRunStatusWithin(t, reg, runID, spec.RunStatusFailed, 3*time.Second)

	attempts, err := reg.ListAttempts(context.Background(), runID, "a")
	if err != nil {
		t.Fatalf("ListAttempts: %v", err)
	}
	ids := map[string]struct{}{}
	for _, a := range attempts {
		ids[a.AttemptID] = struct{}{}
	}
	if len(ids) != 2 {
		t.Fatalf("distinct AttemptIDs = %d, want 2 (each admitted execution is a NEW semantic Attempt, never a reused one)", len(ids))
	}
}

// B2-T10: AttemptRecord.StartedAt populated on a pre-start error does not by itself
// classify the Attempt as started/may-have-started (it does not block an E0 retry).
func TestB2T10_StartedAtOnPreStartErrorIsNotStartProof(t *testing.T) {
	reg := registry.NewMemoryRegistry()
	adapter := &fakeAdapter{failOn: map[string]bool{}, failPrepareOn: map[string]bool{"a": true}}
	engine := NewDagEngine(reg, adapter)

	runID := "b2-t10"
	b2Run(t, engine, reg, runID, spec.Node{NodeID: "a", Image: "busybox:1.36", RetryPolicy: spec.RetryPolicy{MaxAttempts: 2}})
	waitForRunStatusWithin(t, reg, runID, spec.RunStatusFailed, 3*time.Second)

	attempts, err := reg.ListAttempts(context.Background(), runID, "a")
	if err != nil {
		t.Fatalf("ListAttempts: %v", err)
	}
	sawStartedAtOnPreStartError := false
	for _, a := range attempts {
		if a.StartedAt != nil && a.TerminalFailureReason == "backend_prepare_error" {
			sawStartedAtOnPreStartError = true
		}
		if a.BackendHandleJSON != "" || a.SubmissionWindowOpenedAt != nil {
			t.Fatalf("pre-start attempt %s unexpectedly carries submission evidence", a.AttemptID)
		}
	}
	if !sawStartedAtOnPreStartError {
		t.Fatalf("expected a pre-start (backend_prepare_error) attempt with StartedAt set")
	}
	node, _ := reg.GetNode(context.Background(), runID, "a")
	// The E0 retry still happened despite StartedAt being set on the first attempt.
	if node.AttemptCount != 2 {
		t.Fatalf("AttemptCount = %d, want 2 (StartedAt on a pre-start error must not suppress an E0 retry)", node.AttemptCount)
	}
	if got := adapter.startCount("a"); got != 0 {
		t.Fatalf("StartNode calls = %d, want 0 (user code never started)", got)
	}
}

// B2-T11: MaxAttempts>1 alone never turns a post-start (E3) failure into a retry;
// a large budget does not manufacture re-execution authority.
func TestB2T11_MaxAttemptsAloneNeverAuthorizesReexecution(t *testing.T) {
	reg := registry.NewMemoryRegistry()
	// Authoritative backend-reported failure (may-have-started, E3): a large budget
	// must not manufacture a new user-code execution.
	adapter := &fakeAdapter{
		failOn:      map[string]bool{},
		waitResults: map[string]backend.ExecutionResult{"a": {Succeeded: false, TerminalStopCause: "failed", TerminalFailureReason: "user_code_failed"}},
	}
	engine := NewDagEngine(reg, adapter)

	runID := "b2-t11"
	b2Run(t, engine, reg, runID, spec.Node{NodeID: "a", Image: "busybox:1.36", RetryPolicy: spec.RetryPolicy{MaxAttempts: 5}})
	waitForRunStatusWithin(t, reg, runID, spec.RunStatusFailed, 3*time.Second)

	node, _ := reg.GetNode(context.Background(), runID, "a")
	if node.AttemptCount != 1 {
		t.Fatalf("AttemptCount = %d, want 1 (MaxAttempts=5 must not re-execute a may-have-started failure)", node.AttemptCount)
	}
	if got := adapter.startCount("a"); got != 1 {
		t.Fatalf("StartNode calls = %d, want 1", got)
	}
}

// B2-T12: with no explicit retry-safe/idempotent effect contract configured (the
// current default — no such schema exists), a post-start failure defaults to safe:
// user code is not re-executed. The extension point for a future contract remains
// additive and absent here.
func TestB2T12_DefaultSafeWithoutRetrySafeContract(t *testing.T) {
	reg := registry.NewMemoryRegistry()
	adapter := &fakeAdapter{
		failOn:      map[string]bool{},
		waitResults: map[string]backend.ExecutionResult{"a": {Succeeded: false, TerminalStopCause: "failed", TerminalFailureReason: "user_code_failed"}},
	}
	engine := NewDagEngine(reg, adapter)

	runID := "b2-t12"
	b2Run(t, engine, reg, runID, spec.Node{NodeID: "a", Image: "busybox:1.36", RetryPolicy: spec.RetryPolicy{MaxAttempts: 4}})
	waitForRunStatusWithin(t, reg, runID, spec.RunStatusFailed, 3*time.Second)

	node, _ := reg.GetNode(context.Background(), runID, "a")
	if got := adapter.startCount("a"); got != 1 {
		t.Fatalf("StartNode calls = %d, want 1 (default-safe: no re-execution without an explicit retry-safe effect contract)", got)
	}
	if node.AttemptCount != 1 {
		t.Fatalf("AttemptCount = %d, want 1", node.AttemptCount)
	}
}
