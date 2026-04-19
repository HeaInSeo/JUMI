package executor

import (
	"context"
	"testing"
	"time"

	"github.com/HeaInSeo/JUMI/pkg/registry"
	"github.com/HeaInSeo/JUMI/pkg/spec"
)

func TestDagEngineFastFailCancelsRunningSiblingAndSkipsDownstream(t *testing.T) {
	reg := registry.NewMemoryRegistry()
	adapter := &fakeAdapter{
		failOn: map[string]bool{"b1": true},
		waitCh: map[string]chan struct{}{"b2": make(chan struct{})},
	}
	engine := NewDagEngine(reg, adapter)
	specInput := spec.ExecutableRunSpec{
		Run: spec.RunMetadata{RunID: "run-fastfail", SubmittedAt: time.Now().UTC(), FailurePolicy: spec.FailurePolicy{Mode: "fail-fast"}},
		Graph: spec.Graph{
			Nodes: []spec.Node{
				{NodeID: "a", Image: "busybox:1.36"},
				{NodeID: "b1", Image: "busybox:1.36"},
				{NodeID: "b2", Image: "busybox:1.36"},
				{NodeID: "c", Image: "busybox:1.36"},
			},
			Edges: [][]string{{"a", "b1"}, {"a", "b2"}, {"b1", "c"}, {"b2", "c"}},
		},
	}
	record := spec.RunRecord{RunID: specInput.Run.RunID, Status: spec.RunStatusAccepted, AcceptedAt: time.Now().UTC(), Spec: specInput}
	nodes := []spec.NodeRecord{
		{RunID: record.RunID, NodeID: "a", Status: spec.NodeStatusPending},
		{RunID: record.RunID, NodeID: "b1", Status: spec.NodeStatusPending},
		{RunID: record.RunID, NodeID: "b2", Status: spec.NodeStatusPending},
		{RunID: record.RunID, NodeID: "c", Status: spec.NodeStatusPending},
	}
	if err := reg.CreateRun(context.Background(), record, nodes); err != nil {
		t.Fatalf("CreateRun() error = %v", err)
	}
	if err := engine.Admit(context.Background(), record); err != nil {
		t.Fatalf("Admit() error = %v", err)
	}
	waitForRunStatus(t, reg, record.RunID, spec.RunStatusFailed)
	runNodes, err := reg.ListNodes(context.Background(), record.RunID)
	if err != nil {
		t.Fatalf("ListNodes() error = %v", err)
	}
	statuses := map[string]spec.NodeStatus{}
	for _, node := range runNodes {
		statuses[node.NodeID] = node.Status
	}
	if statuses["b1"] != spec.NodeStatusFailed {
		t.Fatalf("node b1 status = %q, want Failed", statuses["b1"])
	}
	if statuses["b2"] != spec.NodeStatusCanceled {
		t.Fatalf("node b2 status = %q, want Canceled", statuses["b2"])
	}
	if statuses["c"] != spec.NodeStatusSkipped {
		t.Fatalf("node c status = %q, want Skipped", statuses["c"])
	}
	adapter.mu.Lock()
	defer adapter.mu.Unlock()
	if !adapter.canceled["b2"] {
		t.Fatal("expected adapter cancel for running sibling b2")
	}
	events, err := reg.ListEvents(context.Background(), record.RunID, 20)
	if err != nil {
		t.Fatalf("ListEvents() error = %v", err)
	}
	foundFastFail := false
	for _, event := range events {
		if event.Type == "run.fast_fail.triggered" {
			foundFastFail = true
			break
		}
	}
	if !foundFastFail {
		t.Fatal("expected run.fast_fail.triggered event")
	}
}
