package backend

import (
	"context"
	"errors"
	"testing"

	"github.com/HeaInSeo/JUMI/pkg/spec"
	spruntime "github.com/HeaInSeo/spawner/pkg/runtime"
	batchv1 "k8s.io/api/batch/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

// labelAttemptMarkerProjection is the truncated, label-safe projection of the
// marker. It is deliberately NOT used for ownership equality; these tests assert
// ownership is decided solely by the full jumi.io/attempt-marker annotation.
const labelAttemptMarkerProjection = "spawner.io/attempt-marker"

// fakeIdentityRuntime is a minimal spruntime.Runtime whose ResolveIdentity
// returns a fixed identity (or error). No other method is exercised by
// ResolveByIdentity, which is a read-only find that never submits/watches/cancels.
type fakeIdentityRuntime struct {
	id  spruntime.BackendIdentity
	err error
}

func (f fakeIdentityRuntime) SubmitAttempt(context.Context, spruntime.AttemptRequest) (spruntime.AttemptHandle, error) {
	return spruntime.AttemptHandle{}, errors.New("SubmitAttempt must not be called by ResolveByIdentity")
}

func (f fakeIdentityRuntime) WatchAttempt(context.Context, spruntime.AttemptHandle) (<-chan spruntime.AttemptEvent, error) {
	return nil, errors.New("WatchAttempt must not be called by ResolveByIdentity")
}

func (f fakeIdentityRuntime) CancelAttempt(context.Context, spruntime.AttemptHandle) error {
	return errors.New("CancelAttempt must not be called by ResolveByIdentity")
}

func (f fakeIdentityRuntime) ResolveIdentity(string) (spruntime.BackendIdentity, error) {
	return f.id, f.err
}

const (
	testResolveNS      = "jumi-runs"
	testResolveJob     = "spw-0123456789abcdef0123456789abcdef"
	testResolveMarker  = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	testResolveAttempt = "run1:nodeA:1"
)

func testIdentity() spruntime.BackendIdentity {
	return spruntime.BackendIdentity{Namespace: testResolveNS, JobName: testResolveJob, AttemptMarker: testResolveMarker}
}

// newResolverAdapterWithJobs builds an adapter whose runtime resolves testIdentity
// and whose fake clientset is seeded with the given Jobs.
func newResolverAdapterWithJobs(id spruntime.BackendIdentity, objs ...runtime.Object) *SpawnerK8sAdapter {
	return NewSpawnerK8sAdapterWithRuntime(
		fakeIdentityRuntime{id: id},
		testResolveNS,
		fake.NewSimpleClientset(objs...),
		nil,
		nil,
	)
}

func TestResolveByIdentity_FoundMatchingAnnotation(t *testing.T) {
	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:   testResolveNS,
			Name:        testResolveJob,
			UID:         types.UID("job-uid-123"),
			Annotations: map[string]string{annotationAttemptMarker: testResolveMarker},
			Labels:      map[string]string{labelQueueName: "q1"},
		},
	}
	adapter := newResolverAdapterWithJobs(testIdentity(), job)

	handle, outcome, err := adapter.ResolveByIdentity(context.Background(), spec.RunRecord{}, spec.Node{}, testResolveAttempt)
	if err != nil {
		t.Fatalf("ResolveByIdentity error: %v", err)
	}
	if outcome != ResolveFound {
		t.Fatalf("outcome = %v, want ResolveFound", outcome)
	}
	rh, ok := handle.(runtimeHandle)
	if !ok {
		t.Fatalf("handle type = %T, want runtimeHandle", handle)
	}
	if rh.handle.BackendRef.UID != "job-uid-123" {
		t.Fatalf("handle UID = %q, want job-uid-123 (Job UID)", rh.handle.BackendRef.UID)
	}
	if rh.handle.BackendRef.Namespace != testResolveNS || rh.handle.BackendRef.Name != testResolveJob {
		t.Fatalf("handle ref = %s/%s, want %s/%s", rh.handle.BackendRef.Namespace, rh.handle.BackendRef.Name, testResolveNS, testResolveJob)
	}
	if rh.handle.AttemptID != testResolveAttempt {
		t.Fatalf("handle attemptID = %q, want %q", rh.handle.AttemptID, testResolveAttempt)
	}
	if rh.queueName != "q1" {
		t.Fatalf("queueName = %q, want q1 (best-effort projection)", rh.queueName)
	}
}

func TestResolveByIdentity_AbsentNowWhenNotFound(t *testing.T) {
	// No Job seeded -> Get returns NotFound.
	adapter := newResolverAdapterWithJobs(testIdentity())

	handle, outcome, err := adapter.ResolveByIdentity(context.Background(), spec.RunRecord{}, spec.Node{}, testResolveAttempt)
	if err != nil {
		t.Fatalf("ResolveByIdentity error: %v", err)
	}
	if outcome != ResolveAbsentNow {
		t.Fatalf("outcome = %v, want ResolveAbsentNow", outcome)
	}
	if handle != nil {
		t.Fatalf("handle = %v, want nil on ABSENT_NOW", handle)
	}
}

func TestResolveByIdentity_ConflictOnDifferentAnnotation(t *testing.T) {
	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:   testResolveNS,
			Name:        testResolveJob,
			UID:         types.UID("other-uid"),
			Annotations: map[string]string{annotationAttemptMarker: "not-our-marker"},
		},
	}
	adapter := newResolverAdapterWithJobs(testIdentity(), job)

	handle, outcome, err := adapter.ResolveByIdentity(context.Background(), spec.RunRecord{}, spec.Node{}, testResolveAttempt)
	if err != nil {
		t.Fatalf("ResolveByIdentity error: %v", err)
	}
	if outcome != ResolveConflict {
		t.Fatalf("outcome = %v, want ResolveConflict", outcome)
	}
	if handle != nil {
		t.Fatalf("handle = %v, want nil on CONFLICT", handle)
	}
}

// A Job whose truncated spawner.io/attempt-marker LABEL matches but whose
// authoritative jumi.io/attempt-marker ANNOTATION is MISSING must be a CONFLICT
// (fail-closed), proving the label is never used for ownership equality.
func TestResolveByIdentity_ConflictOnMissingAnnotationEvenIfLabelMatches(t *testing.T) {
	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: testResolveNS,
			Name:      testResolveJob,
			UID:       types.UID("mislabeled-uid"),
			// No annotation at all; only the non-authoritative label projection,
			// set to the full marker to make the "label used for ownership" bug
			// (if present) resolve FOUND.
			Labels: map[string]string{labelAttemptMarkerProjection: testResolveMarker},
		},
	}
	adapter := newResolverAdapterWithJobs(testIdentity(), job)

	handle, outcome, err := adapter.ResolveByIdentity(context.Background(), spec.RunRecord{}, spec.Node{}, testResolveAttempt)
	if err != nil {
		t.Fatalf("ResolveByIdentity error: %v", err)
	}
	if outcome != ResolveConflict {
		t.Fatalf("outcome = %v, want ResolveConflict (annotation missing => fail-closed; label never used)", outcome)
	}
	if handle != nil {
		t.Fatalf("handle = %v, want nil (never attach on missing annotation)", handle)
	}
}

func TestResolveByIdentity_UnknownOnGetError(t *testing.T) {
	clientset := fake.NewSimpleClientset()
	clientset.PrependReactor("get", "jobs", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("etcdserver: request timed out")
	})
	adapter := NewSpawnerK8sAdapterWithRuntime(fakeIdentityRuntime{id: testIdentity()}, testResolveNS, clientset, nil, nil)

	handle, outcome, err := adapter.ResolveByIdentity(context.Background(), spec.RunRecord{}, spec.Node{}, testResolveAttempt)
	if outcome != ResolveUnknown {
		t.Fatalf("outcome = %v, want ResolveUnknown", outcome)
	}
	if err == nil {
		t.Fatal("expected fail-closed error on a non-NotFound Get error")
	}
	if handle != nil {
		t.Fatalf("handle = %v, want nil on UNKNOWN", handle)
	}
}

func TestResolveByIdentity_UnknownOnResolveIdentityError(t *testing.T) {
	adapter := NewSpawnerK8sAdapterWithRuntime(
		fakeIdentityRuntime{err: errors.New("naming salt unavailable")},
		testResolveNS,
		fake.NewSimpleClientset(),
		nil,
		nil,
	)

	_, outcome, err := adapter.ResolveByIdentity(context.Background(), spec.RunRecord{}, spec.Node{}, testResolveAttempt)
	if outcome != ResolveUnknown {
		t.Fatalf("outcome = %v, want ResolveUnknown", outcome)
	}
	if err == nil {
		t.Fatal("expected fail-closed error when ResolveIdentity fails")
	}
}

func TestResolveByIdentity_UnknownOnNilRuntime(t *testing.T) {
	adapter := NewSpawnerK8sAdapterWithRuntime(nil, testResolveNS, fake.NewSimpleClientset(), nil, nil)

	_, outcome, err := adapter.ResolveByIdentity(context.Background(), spec.RunRecord{}, spec.Node{}, testResolveAttempt)
	if outcome != ResolveUnknown {
		t.Fatalf("outcome = %v, want ResolveUnknown for nil runtime", outcome)
	}
	if err == nil {
		t.Fatal("expected error for nil runtime")
	}
}

func TestResolveByIdentity_UnknownOnNilClientset(t *testing.T) {
	adapter := NewSpawnerK8sAdapterWithRuntime(fakeIdentityRuntime{id: testIdentity()}, testResolveNS, nil, nil, nil)

	_, outcome, err := adapter.ResolveByIdentity(context.Background(), spec.RunRecord{}, spec.Node{}, testResolveAttempt)
	if outcome != ResolveUnknown {
		t.Fatalf("outcome = %v, want ResolveUnknown for nil clientset", outcome)
	}
	if err == nil {
		t.Fatal("expected error for nil clientset")
	}
}

// F-N2: an empty AttemptMarker must fail closed (ResolveUnknown), never FOUND,
// even when a Job exists whose annotation is also empty/absent (which would
// otherwise compare equal to "").
func TestResolveByIdentity_EmptyMarkerFailsClosed(t *testing.T) {
	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: testResolveNS,
			Name:      testResolveJob,
			UID:       types.UID("empty-marker-uid"),
			// Annotation absent => job.Annotations[key] == "" == id.AttemptMarker
			// would spuriously match without the empty-marker guard.
		},
	}
	id := spruntime.BackendIdentity{Namespace: testResolveNS, JobName: testResolveJob, AttemptMarker: ""}
	adapter := newResolverAdapterWithJobs(id, job)

	handle, outcome, err := adapter.ResolveByIdentity(context.Background(), spec.RunRecord{}, spec.Node{}, testResolveAttempt)
	if outcome != ResolveUnknown {
		t.Fatalf("outcome = %v, want ResolveUnknown (empty marker => fail-closed)", outcome)
	}
	if err == nil {
		t.Fatal("expected error for empty attempt marker")
	}
	if handle != nil {
		t.Fatalf("handle = %v, want nil (never attach on empty marker)", handle)
	}
}
