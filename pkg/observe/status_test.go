package observe

import (
	"context"
	"testing"
	"time"

	"github.com/HeaInSeo/JUMI/pkg/registry"
	"github.com/HeaInSeo/JUMI/pkg/spec"
)

func TestSnapshotFromRegistry(t *testing.T) {
	reg := registry.NewMemoryRegistry()
	run := spec.RunRecord{RunID: "run-1", Status: spec.RunStatusRunning, AcceptedAt: time.Now().UTC(), Spec: spec.ExecutableRunSpec{Run: spec.RunMetadata{RunID: "run-1", SubmittedAt: time.Now().UTC(), FailurePolicy: spec.FailurePolicy{Mode: "fail-fast"}}}}
	nodes := []spec.NodeRecord{
		{RunID: "run-1", NodeID: "a", Status: spec.NodeStatusReady},
		{RunID: "run-1", NodeID: "b", Status: spec.NodeStatusRunning},
	}
	if err := reg.CreateRun(context.Background(), run, nodes); err != nil {
		t.Fatalf("CreateRun() error = %v", err)
	}
	if err := reg.AppendEvent(context.Background(), spec.EventRecord{RunID: "run-1", Type: "node.failed", OccurredAt: time.Now().UTC(), Level: "error", FailureReason: "backend_wait_error"}); err != nil {
		t.Fatalf("AppendEvent() error = %v", err)
	}
	snapshot, err := SnapshotFromRegistry(context.Background(), reg, BackendSnapshot{Ready: true, ReleaseBounded: true, ReleaseInflight: 1, ReleaseSlotsAvailable: 0, ReleaseMaxConcurrent: 1})
	if err != nil {
		t.Fatalf("SnapshotFromRegistry() error = %v", err)
	}
	if snapshot.RunningRuns != 1 {
		t.Fatalf("RunningRuns = %d, want 1", snapshot.RunningRuns)
	}
	if snapshot.ReleaseWaitNodes != 1 {
		t.Fatalf("ReleaseWaitNodes = %d, want 1", snapshot.ReleaseWaitNodes)
	}
	if len(snapshot.RecentErrors) != 1 {
		t.Fatalf("RecentErrors len = %d, want 1", len(snapshot.RecentErrors))
	}
	if !snapshot.ReleaseBounded || snapshot.ReleaseInflight != 1 || snapshot.ReleaseSlotsAvailable != 0 || snapshot.ReleaseMaxConcurrent != 1 {
		t.Fatalf("unexpected release stats: %+v", snapshot)
	}
}
