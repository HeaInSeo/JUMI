package registry

import (
	"context"
	"time"

	"github.com/HeaInSeo/JUMI/pkg/spec"
)

type Registry interface {
	CreateRun(ctx context.Context, record spec.RunRecord, nodes []spec.NodeRecord) error
	GetRun(ctx context.Context, runID string) (spec.RunRecord, error)
	ListRuns(ctx context.Context) ([]spec.RunRecord, error)
	GetNode(ctx context.Context, runID, nodeID string) (spec.NodeRecord, error)
	ListNodes(ctx context.Context, runID string) ([]spec.NodeRecord, error)
	ListAttempts(ctx context.Context, runID, nodeID string) ([]spec.AttemptRecord, error)
	ListEvents(ctx context.Context, runID string, limit int) ([]spec.EventRecord, error)
	UpdateRun(ctx context.Context, runID string, update func(*spec.RunRecord) error) error
	UpdateNode(ctx context.Context, runID, nodeID string, update func(*spec.NodeRecord) error) error
	UpsertAttempt(ctx context.Context, record spec.AttemptRecord) error
	AppendEvent(ctx context.Context, event spec.EventRecord) error

	// --- F3 durable execution-truth operations (Closure Sprint B Packet A) ---

	// GetCurrentAttempt returns the current Attempt of a node (the one named by
	// NodeRecord.CurrentAttemptID). The bool is false when the node has no
	// current Attempt yet (fresh, or reset for a legitimate retry).
	GetCurrentAttempt(ctx context.Context, runID, nodeID string) (spec.AttemptRecord, bool, error)

	// AllocateCurrentAttempt atomically allocates the next Attempt for a node.
	// In ONE transaction it: reads the node; rejects with ErrAttemptNonTerminal
	// if a current Attempt exists and is non-terminal (invariant: never create a
	// replacement Attempt while the current one is unresolved); computes
	// next = AttemptCount+1 with a deterministic Attempt id; INSERTs the Prepared
	// AttemptRecord; and UPDATEs the NodeRecord counter/pointer. Postcondition:
	// all of these facts exist, or none do (all-or-nothing).
	AllocateCurrentAttempt(ctx context.Context, runID, nodeID string) (spec.AttemptRecord, error)

	// OpenSemanticAttempt records the submission-fence crossing for the current
	// reservation: the semantic Attempt opens here and consumes one user-code
	// execution opportunity (AttemptCount++). Callers must verify an opportunity
	// slot is available (AttemptCount < RetryPolicy.MaxAttempts) before calling
	// (F3-B3). It applies the AttemptCount increment and the fence timestamp on the
	// current attempt atomically.
	OpenSemanticAttempt(ctx context.Context, runID, nodeID, attemptID string, openedAt time.Time) error

	// PersistSubmissionFence durably records that the submission window was
	// opened for an Attempt BEFORE crossing the backend side-effect boundary.
	PersistSubmissionFence(ctx context.Context, runID, nodeID, attemptID string, openedAt time.Time) error

	// PersistBackendHandle durably records the authoritative serialized backend
	// handle on the Attempt (and mirrors it onto the node as a compat projection).
	PersistBackendHandle(ctx context.Context, runID, nodeID, attemptID, handleJSON string) error

	// PersistCancellationIntent durably records an accepted cancellation intent
	// on the Attempt until terminal reconciliation confirms terminal truth.
	PersistCancellationIntent(ctx context.Context, runID, nodeID, attemptID string, requestedAt time.Time, reason string) error

	// PersistProcessCompleted durably records that the user process for an Attempt
	// completed successfully and only platform finalization remains (F3-B2 / #46).
	// Once set, recovery reconciles finalization on the same execution and never
	// re-runs user code.
	PersistProcessCompleted(ctx context.Context, runID, nodeID, attemptID string, completedAt time.Time) error
}
