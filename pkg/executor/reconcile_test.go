package executor

import (
	"testing"
	"time"

	"github.com/HeaInSeo/JUMI/pkg/spec"
)

func ts() *time.Time {
	v := time.Now().UTC()
	return &v
}

func TestClassifyReconcile_Fresh(t *testing.T) {
	node := spec.NodeRecord{RunID: "r", NodeID: "n", Status: spec.NodeStatusPending}
	if got := ClassifyReconcile(node, spec.AttemptRecord{}, false); got != ReconcileFresh {
		t.Fatalf("no current attempt -> %v, want fresh", got)
	}
	// Retry reset: node Pending with cleared CurrentAttemptID -> fresh.
	node2 := spec.NodeRecord{RunID: "r", NodeID: "n", Status: spec.NodeStatusPending, AttemptCount: 1, CurrentAttemptID: ""}
	if got := ClassifyReconcile(node2, spec.AttemptRecord{}, false); got != ReconcileFresh {
		t.Fatalf("retry reset -> %v, want fresh", got)
	}
}

// F3-T02: crash after allocation before fence → same Attempt continues.
func TestClassifyReconcile_F3T02_ResumePreFence(t *testing.T) {
	node := spec.NodeRecord{
		RunID: "r", NodeID: "n",
		Status:           spec.NodeStatusReady, // allocated, not yet started
		AttemptCount:     1,
		CurrentAttemptID: "r-n-attempt-1",
	}
	attempt := spec.AttemptRecord{
		RunID: "r", NodeID: "n", AttemptID: "r-n-attempt-1",
		Status:    spec.AttemptStatusPrepared,
		StartedAt: ts(),
		// no SubmissionWindowOpenedAt, no BackendHandleJSON
	}
	if got := ClassifyReconcile(node, attempt, true); got != ReconcileResumePreFence {
		t.Fatalf("classify = %v, want resume_pre_fence (same attempt continues)", got)
	}
}

// F3-T03: fence durable + crash before StartNode response → new Attempt forbidden.
func TestClassifyReconcile_F3T03_ResolveByIdentity(t *testing.T) {
	node := spec.NodeRecord{
		RunID: "r", NodeID: "n",
		Status:           spec.NodeStatusStarting, // crossed toward backend boundary
		AttemptCount:     1,
		CurrentAttemptID: "r-n-attempt-1",
	}
	attempt := spec.AttemptRecord{
		RunID: "r", NodeID: "n", AttemptID: "r-n-attempt-1",
		Status:                   spec.AttemptStatusPrepared,
		StartedAt:                ts(),
		SubmissionWindowOpenedAt: ts(), // fence crossed, but no handle persisted
	}
	got := ClassifyReconcile(node, attempt, true)
	if got != ReconcileResolveByIdentity {
		t.Fatalf("classify = %v, want resolve_by_identity (no blind replacement Attempt)", got)
	}
}

func TestClassifyReconcile_Reattach(t *testing.T) {
	node := spec.NodeRecord{
		RunID: "r", NodeID: "n",
		Status:                   spec.NodeStatusRunning,
		AttemptCount:             1,
		CurrentAttemptID:         "r-n-attempt-1",
		CurrentAttemptHandleJSON: `{"job":"j1"}`,
	}
	attempt := spec.AttemptRecord{
		RunID: "r", NodeID: "n", AttemptID: "r-n-attempt-1",
		Status:                   spec.AttemptStatusStarted,
		SubmissionWindowOpenedAt: ts(),
		BackendHandleJSON:        `{"job":"j1"}`,
	}
	if got := ClassifyReconcile(node, attempt, true); got != ReconcileReattach {
		t.Fatalf("classify = %v, want reattach", got)
	}
}

func TestClassifyReconcile_TerminalRepair(t *testing.T) {
	// Node already terminal.
	nodeDone := spec.NodeRecord{RunID: "r", NodeID: "n", Status: spec.NodeStatusSucceeded, CurrentAttemptID: "r-n-attempt-1"}
	attDone := spec.AttemptRecord{AttemptID: "r-n-attempt-1", Status: spec.AttemptStatusCompleted}
	if got := ClassifyReconcile(nodeDone, attDone, true); got != ReconcileTerminalRepair {
		t.Fatalf("terminal node -> %v, want terminal_repair", got)
	}
	// Attempt terminal but node projection stale (non-terminal): attempt is authority.
	nodeStale := spec.NodeRecord{RunID: "r", NodeID: "n", Status: spec.NodeStatusRunning, CurrentAttemptID: "r-n-attempt-1"}
	attErr := spec.AttemptRecord{AttemptID: "r-n-attempt-1", Status: spec.AttemptStatusErrored}
	if got := ClassifyReconcile(nodeStale, attErr, true); got != ReconcileTerminalRepair {
		t.Fatalf("stale node + terminal attempt -> %v, want terminal_repair", got)
	}
}

// RME-I1 Correction 1: a terminal Errored Attempt that durably recorded a PRE-FENCE
// replay-safe realization re-attempt basis (RealizationReattemptableAt, fence not
// crossed) is re-realized from durable truth across crash -> restart -> reconcile,
// not terminalized. The basis is honored ONLY pre-fence, preserving post-fence
// no-rerun (F3-B2) and never consuming the user-code opportunity budget (F3-B3).
func TestClassifyReconcile_RMEI1_PreFenceRealizationReattempt(t *testing.T) {
	// Crash window: attempt Errored + durable re-attempt basis, node projection not
	// yet reset to Pending. Must re-realize (fresh next Attempt), NOT terminalize.
	nodeStale := spec.NodeRecord{RunID: "r", NodeID: "n", Status: spec.NodeStatusStarting, CurrentAttemptID: "r-n-attempt-1"}
	attReattempt := spec.AttemptRecord{
		RunID: "r", NodeID: "n", AttemptID: "r-n-attempt-1",
		Status:                     spec.AttemptStatusErrored,
		RealizationReattemptableAt: ts(),
		// SubmissionWindowOpenedAt nil: fence NOT crossed.
	}
	if got := ClassifyReconcile(nodeStale, attReattempt, true); got != ReconcileFresh {
		t.Fatalf("pre-fence realization re-attempt basis -> %v, want fresh (re-realize)", got)
	}

	// Defense-in-depth: the basis must NEVER re-run a post-fence outcome. If the fence
	// was crossed, the terminal Errored Attempt is authoritative -> terminal repair.
	attPostFence := attReattempt
	attPostFence.SubmissionWindowOpenedAt = ts()
	if got := ClassifyReconcile(nodeStale, attPostFence, true); got != ReconcileTerminalRepair {
		t.Fatalf("post-fence + basis -> %v, want terminal_repair (no-rerun preserved)", got)
	}

	// No basis: unchanged authority of the terminal Errored Attempt.
	attNoBasis := spec.AttemptRecord{RunID: "r", NodeID: "n", AttemptID: "r-n-attempt-1", Status: spec.AttemptStatusErrored}
	if got := ClassifyReconcile(nodeStale, attNoBasis, true); got != ReconcileTerminalRepair {
		t.Fatalf("terminal errored w/o basis -> %v, want terminal_repair", got)
	}

	// An already-terminal NODE still repairs regardless of the basis (rule 1 wins).
	nodeTerminal := spec.NodeRecord{RunID: "r", NodeID: "n", Status: spec.NodeStatusFailed, CurrentAttemptID: "r-n-attempt-1"}
	if got := ClassifyReconcile(nodeTerminal, attReattempt, true); got != ReconcileTerminalRepair {
		t.Fatalf("terminal node + basis -> %v, want terminal_repair", got)
	}
}
