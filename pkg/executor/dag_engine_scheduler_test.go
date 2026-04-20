package executor

import (
	"context"
	"testing"
	"time"

	"github.com/HeaInSeo/JUMI/pkg/backend"
	"github.com/HeaInSeo/JUMI/pkg/registry"
	"github.com/HeaInSeo/JUMI/pkg/spec"
)

type schedulerPendingAdapter struct{ fakeAdapter }

func (s *schedulerPendingAdapter) ObserveNode(_ context.Context, _ backend.Handle) (*backend.OptionalKueueInfo, error) {
	return &backend.OptionalKueueInfo{
		Observed:            true,
		QueueName:           "poc-standard-lq",
		WorkloadName:        "wl-1",
		Admitted:            true,
		PodName:             "pod-1",
		Scheduled:           false,
		UnschedulableReason: "0/1 nodes are available",
	}, nil
}

func TestDagEngineRecordsSchedulerPendingSignal(t *testing.T) {
	reg := registry.NewMemoryRegistry()
	adapter := &schedulerPendingAdapter{fakeAdapter: fakeAdapter{failOn: map[string]bool{}}}
	engine := NewDagEngine(reg, adapter)
	specInput := spec.ExecutableRunSpec{
		Run:   spec.RunMetadata{RunID: "run-scheduler", SubmittedAt: time.Now().UTC(), FailurePolicy: spec.FailurePolicy{Mode: "fail-fast"}},
		Graph: spec.Graph{Nodes: []spec.Node{{NodeID: "a", Image: "busybox:1.36", Kueue: &spec.KueueHints{QueueName: "poc-standard-lq"}}}},
	}
	record := spec.RunRecord{RunID: specInput.Run.RunID, Status: spec.RunStatusAccepted, AcceptedAt: time.Now().UTC(), Spec: specInput}
	nodes := []spec.NodeRecord{{RunID: record.RunID, NodeID: "a", Status: spec.NodeStatusPending}}
	if err := reg.CreateRun(context.Background(), record, nodes); err != nil {
		t.Fatalf("CreateRun() error = %v", err)
	}
	if err := engine.Admit(context.Background(), record); err != nil {
		t.Fatalf("Admit() error = %v", err)
	}
	waitForRunStatus(t, reg, record.RunID, spec.RunStatusSucceeded)
	events, err := reg.ListEvents(context.Background(), record.RunID, 20)
	if err != nil {
		t.Fatalf("ListEvents() error = %v", err)
	}
	found := false
	for _, event := range events {
		if event.Type == "node.scheduler.pending" {
			found = true
			if event.Message != "0/1 nodes are available" {
				t.Fatalf("unexpected scheduler pending message: %q", event.Message)
			}
		}
	}
	if !found {
		t.Fatal("expected node.scheduler.pending event")
	}
}
