package executor

import (
	"context"
	"log"
	"time"

	"github.com/HeaInSeo/JUMI/pkg/spec"
)

// ReconcileDecision is the outcome of classifying a node's current durable
// execution truth on restart / unknown-outcome. It is derived purely from
// durable facts (Attempt terminal status, submission fence, backend handle,
// Node projection) — there are no broad canonical enums persisted for it.
type ReconcileDecision int

const (
	// ReconcileFresh: no current Attempt (fresh node, or reset for a legitimate
	// retry after the previous Attempt reached terminal truth). A new Attempt
	// may be allocated.
	ReconcileFresh ReconcileDecision = iota
	// ReconcileResumePreFence: a non-terminal current Attempt exists but the
	// submission fence was NOT crossed and no backend handle exists — the
	// backend side-effect boundary was positively not crossed, so the SAME
	// Attempt may continue (no replacement Attempt).
	ReconcileResumePreFence
	// ReconcileReattach: a non-terminal current Attempt has a persisted backend
	// handle — observe/cancel/wait the SAME backend execution.
	ReconcileReattach
	// ReconcileResolveByIdentity: the submission fence was crossed but no handle
	// was persisted — the backend submit outcome is unknown-but-possibly-effected.
	// Resolution MUST be by deterministic Attempt identity; a blind replacement
	// Attempt is forbidden while this is unresolved.
	ReconcileResolveByIdentity
	// ReconcileTerminalRepair: the current Attempt already reached terminal
	// truth (or the node is already terminal). Do NOT execute; repair any stale
	// Node/Run projection only.
	ReconcileTerminalRepair
)

func (d ReconcileDecision) String() string {
	switch d {
	case ReconcileFresh:
		return "fresh"
	case ReconcileResumePreFence:
		return "resume_pre_fence"
	case ReconcileReattach:
		return "reattach"
	case ReconcileResolveByIdentity:
		return "resolve_by_identity"
	case ReconcileTerminalRepair:
		return "terminal_repair"
	default:
		return "unknown"
	}
}

// ClassifyReconcile derives the reconcile decision from durable facts alone.
//
// Ordering of the checks encodes the F3 reconcile algorithm:
//  1. Node already terminal -> terminal repair (projection only, never execute).
//  2. No current Attempt -> fresh allocation is permitted.
//  3. Current Attempt terminal -> Attempt is the execution authority; repair the
//     (stale) non-terminal Node projection, never execute.
//  4. Backend handle present -> reattach to the same backend execution.
//  5. Submission fence crossed (no handle) -> resolve by deterministic identity;
//     NO blind replacement Attempt while unresolved.
//  6. Otherwise -> side-effect boundary positively not crossed -> same Attempt
//     may continue.
func ClassifyReconcile(node spec.NodeRecord, attempt spec.AttemptRecord, hasAttempt bool) ReconcileDecision {
	if node.Status.IsTerminal() {
		return ReconcileTerminalRepair
	}
	// A terminal Attempt is the execution authority even if the Node projection
	// is stale; repair rather than execute.
	if hasAttempt && attempt.Status.IsTerminal() {
		return ReconcileTerminalRepair
	}
	// A persisted backend handle (authoritatively on the Attempt, or mirrored on
	// the Node as a compat projection) means the same backend execution can be
	// reattached. This is checked before the fresh gate so a node-level handle
	// alone still drives reattach.
	if node.CurrentAttemptHandleJSON != "" || (hasAttempt && attempt.BackendHandleJSON != "") {
		return ReconcileReattach
	}
	if !hasAttempt || node.CurrentAttemptID == "" {
		return ReconcileFresh
	}
	if attempt.SubmissionWindowOpenedAt != nil {
		return ReconcileResolveByIdentity
	}
	return ReconcileResumePreFence
}

// Recover performs the startup reconcile-first sweep. For every non-terminal
// run it re-admits execution: the DAG re-runs, and each node runner classifies
// its own current Attempt (via ClassifyReconcile) before doing anything, so
// already-terminal nodes are repaired rather than re-executed and in-flight
// Attempts are reattached — never replaced while unresolved.
//
// This is the startup component of the target model. The periodic bounded
// non-terminal sweep and full orphan-convergence are deferred (see PR notes).
func (e *DagEngine) Recover(ctx context.Context) error {
	runs, err := e.registry.ListRuns(ctx)
	if err != nil {
		return err
	}
	resumed := 0
	for _, run := range runs {
		if run.Status.IsTerminal() {
			continue
		}
		appendEvent(ctx, e.registry, spec.EventRecord{
			RunID:      run.RunID,
			Type:       "run.recovery.resumed",
			OccurredAt: time.Now().UTC(),
			Level:      "info",
			Message:    "run resumed by startup reconcile sweep",
		})
		runCtx := context.WithoutCancel(ctx)
		// #nosec G118 -- recovered run execution must outlive the startup context.
		go e.executeRun(runCtx, run.RunID)
		resumed++
	}
	if resumed > 0 {
		log.Printf("jumi: startup reconcile resumed %d non-terminal run(s)", resumed)
	}
	return nil
}
