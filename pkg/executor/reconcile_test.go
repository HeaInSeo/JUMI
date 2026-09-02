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
