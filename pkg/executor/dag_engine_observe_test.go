package executor

import (
	"context"
	"testing"
	"time"

	"github.com/HeaInSeo/JUMI/pkg/registry"
	"github.com/HeaInSeo/JUMI/pkg/spec"
)

func TestDagEngineRecordsAttemptsAndEvents(t *testing.T) {
	reg := registry.NewMemoryRegistry()
	adapter := &fakeAdapter{failOn: map[string]bool{}}
	engine := NewDagEngine(reg, adapter)
	specInput := spec.ExecutableRunSpec{
		Run:   spec.RunMetadata{RunID: "run-observe", SubmittedAt: time.Now().UTC(), FailurePolicy: spec.FailurePolicy{Mode: "fail-fast"}},
		Graph: spec.Graph{Nodes: []spec.Node{{NodeID: "a", Image: "busybox:1.36"}}},
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
	attempts, err := reg.ListAttempts(context.Background(), record.RunID, "a")
	if err != nil {
		t.Fatalf("ListAttempts() error = %v", err)
	}
	if len(attempts) != 1 {
		t.Fatalf("attempt count = %d, want 1", len(attempts))
	}
	if attempts[0].Status != spec.AttemptStatusCompleted {
		t.Fatalf("attempt status = %q, want %q", attempts[0].Status, spec.AttemptStatusCompleted)
	}
	events, err := reg.ListEvents(context.Background(), record.RunID, 20)
	if err != nil {
		t.Fatalf("ListEvents() error = %v", err)
	}
	if len(events) == 0 {
		t.Fatal("expected recorded events")
	}
	foundNodeRunning := false
	foundRunCompleted := false
	for _, event := range events {
		if event.Type == "node.running" {
			foundNodeRunning = true
		}
		if event.Type == "run.completed" {
			foundRunCompleted = true
		}
	}
	if !foundNodeRunning {
		t.Fatal("expected node.running event")
	}
	if !foundRunCompleted {
		t.Fatal("expected run.completed event")
	}
}

func TestDagEngineRecordsReleaseWaitEvent(t *testing.T) {
	reg := registry.NewMemoryRegistry()
	adapter := &fakeAdapter{failOn: map[string]bool{}, startDelay: 250 * time.Millisecond}
	engine := NewDagEngine(reg, adapter)
	specInput := spec.ExecutableRunSpec{
		Run:   spec.RunMetadata{RunID: "run-release-wait", SubmittedAt: time.Now().UTC(), FailurePolicy: spec.FailurePolicy{Mode: "fail-fast"}},
		Graph: spec.Graph{Nodes: []spec.Node{{NodeID: "a", Image: "busybox:1.36"}}},
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
		if event.Type == "node.release.waited" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected node.release.waited event")
	}
}
