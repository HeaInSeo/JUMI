package executor

import (
	"context"
	"testing"
	"time"

	"github.com/HeaInSeo/JUMI/pkg/backend"
	"github.com/HeaInSeo/JUMI/pkg/registry"
	"github.com/HeaInSeo/JUMI/pkg/spec"
)

type kueueAdapter struct{ fakeAdapter }

func (k *kueueAdapter) ObserveNode(_ context.Context, _ backend.Handle) (*backend.OptionalKueueInfo, error) {
	return &backend.OptionalKueueInfo{
		Observed:      true,
		QueueName:     "poc-standard-lq",
		PendingReason: "quota pending",
		Admitted:      false,
	}, nil
}

func TestDagEngineRecordsKueuePendingBottleneck(t *testing.T) {
	reg := registry.NewMemoryRegistry()
	adapter := &kueueAdapter{fakeAdapter: fakeAdapter{failOn: map[string]bool{}}}
	engine := NewDagEngine(reg, adapter)
	specInput := spec.ExecutableRunSpec{
		Run:   spec.RunMetadata{RunID: "run-kueue", SubmittedAt: time.Now().UTC(), FailurePolicy: spec.FailurePolicy{Mode: "fail-fast"}},
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
	runNodes, err := reg.ListNodes(context.Background(), record.RunID)
	if err != nil {
		t.Fatalf("ListNodes() error = %v", err)
	}
	if runNodes[0].CurrentBottleneckLocation != "" {
		t.Fatalf("final bottleneck = %q, want empty terminal bottleneck", runNodes[0].CurrentBottleneckLocation)
	}
	events, err := reg.ListEvents(context.Background(), record.RunID, 20)
	if err != nil {
		t.Fatalf("ListEvents() error = %v", err)
	}
	found := false
	for _, event := range events {
		if event.Type == "node.kueue.pending" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected node.kueue.pending event")
	}
}
