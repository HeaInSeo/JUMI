package backend

import (
	"context"
	"fmt"

	"github.com/HeaInSeo/JUMI/pkg/spec"
	spruntime "github.com/HeaInSeo/spawner/pkg/runtime"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// annotationAttemptMarker is the AUTHORITATIVE ownership annotation on a backend
// Job. It holds the full 64-char AttemptMarker (a K8s label cannot). Ownership of
// a found Job is decided ONLY by a full-string compare against this annotation;
// the truncated "spawner.io/attempt-marker" label is a non-authoritative
// projection and is never used for ownership equality (F3-B1 marker ruling).
const annotationAttemptMarker = "jumi.io/attempt-marker"

// labelQueueName is the Kueue queue-name label, recovered best-effort onto the
// resolved handle so post-attach observation keeps working. It is a projection
// only and plays no part in ownership.
const labelQueueName = "kueue.x-k8s.io/queue-name"

// ResolveOutcome classifies the result of a read-only backend identity lookup for
// an attempt whose durable backend truth was lost (submission fence crossed, no
// surviving handle). It never reflects a create-or-get: the lookup is a pure Get.
type ResolveOutcome int

const (
	// ResolveUnknown is the fail-closed default: the lookup could not be
	// completed (timeout / forbidden / other error). Recovery must not attach,
	// create, or replace on this outcome.
	ResolveUnknown ResolveOutcome = iota
	// ResolveFound: the Job exists AND its authoritative jumi.io/attempt-marker
	// annotation equals the identity's AttemptMarker (full string compare). The
	// returned Handle is a usable durable handle for the SAME attempt.
	ResolveFound
	// ResolveAbsentNow: the Job is NotFound right now. This is NOT proof it never
	// ran; deciding whether the Attempt may be re-executed is out of scope (#46).
	ResolveAbsentNow
	// ResolveConflict: the Job exists but its marker annotation is missing or does
	// not equal the identity's AttemptMarker — ownership mismatch. Never attach.
	ResolveConflict
)

func (o ResolveOutcome) String() string {
	switch o {
	case ResolveFound:
		return "found"
	case ResolveAbsentNow:
		return "absent_now"
	case ResolveConflict:
		return "conflict"
	default:
		return "unknown"
	}
}

// AttemptResolver is an optional capability an Adapter may implement to support
// F3-B1 read-only backend identity recovery. ResolveByIdentity finds — never
// creates — the backend Job for an attempt whose durable handle did not survive a
// crash. Implementations MUST perform a read-only lookup only: no Create, no
// SubmitAttempt, no create-or-get. Ownership is decided solely by the full
// jumi.io/attempt-marker annotation.
type AttemptResolver interface {
	ResolveByIdentity(ctx context.Context, run spec.RunRecord, node spec.Node, attemptID string) (Handle, ResolveOutcome, error)
}

// ResolveByIdentity implements AttemptResolver for the spawner Runtime path.
//
// It is a read-only find-by-identity:
//  1. rt.ResolveIdentity(attemptID) derives {Namespace, JobName, AttemptMarker}
//     from the single naming source — JUMI never re-derives the naming/marker.
//  2. a single read-only BatchV1().Jobs(Namespace).Get(JobName) — never a Create.
//  3. classify by NotFound / marker annotation equality.
//
// The one rule: this method never creates a Job to discover whether one exists.
func (a *SpawnerK8sAdapter) ResolveByIdentity(ctx context.Context, _ spec.RunRecord, _ spec.Node, attemptID string) (Handle, ResolveOutcome, error) {
	if a.rt == nil {
		return nil, ResolveUnknown, fmt.Errorf("resolve by identity: runtime not configured")
	}
	if a.clientset == nil {
		return nil, ResolveUnknown, fmt.Errorf("resolve by identity: clientset not configured")
	}
	id, err := a.rt.ResolveIdentity(attemptID)
	if err != nil {
		return nil, ResolveUnknown, fmt.Errorf("resolve identity for attempt %s: %w", attemptID, err)
	}
	// READ-ONLY Get. There is deliberately no Create/SubmitAttempt on any path
	// here: recovery must never create a Job to discover whether one exists.
	job, err := a.clientset.BatchV1().Jobs(id.Namespace).Get(ctx, id.JobName, metav1.GetOptions{})
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil, ResolveAbsentNow, nil
		}
		// timeout / forbidden / other → fail-closed.
		return nil, ResolveUnknown, fmt.Errorf("read-only get job %s/%s: %w", id.Namespace, id.JobName, err)
	}
	// F-N2: defense in depth. An empty AttemptMarker cannot prove ownership of any
	// Job (an unset annotation would spuriously compare equal to ""), so fail
	// closed regardless of the runtime naming contract rather than depend on it.
	if id.AttemptMarker == "" {
		return nil, ResolveUnknown, fmt.Errorf("resolve identity for attempt %s: empty attempt marker", attemptID)
	}
	// Ownership equality is decided ONLY by the authoritative full-marker
	// annotation. A missing or differing annotation is a conflict; never attach to
	// a Job we cannot prove is this attempt's.
	if job.Annotations[annotationAttemptMarker] != id.AttemptMarker {
		return nil, ResolveConflict, nil
	}
	handle := runtimeHandle{
		handle: spruntime.AttemptHandle{
			AttemptID:  attemptID,
			BackendRef: spruntime.NewK8sJobBackendRef(id.Namespace, id.JobName, string(job.UID)),
		},
		queueName: job.Labels[labelQueueName],
	}
	return handle, ResolveFound, nil
}
