package api

import (
	"context"
	"testing"
	"time"

	"github.com/HeaInSeo/JUMI/pkg/executor"
	"github.com/HeaInSeo/JUMI/pkg/registry"
	"github.com/HeaInSeo/JUMI/pkg/spec"
)

func TestServiceSubmitGetAndCancelRun(t *testing.T) {
	reg := registry.NewMemoryRegistry()
	eng := executor.NewNoopEngine(reg)
	svc := NewService(reg, eng)

	specInput := spec.ExecutableRunSpec{
		Run: spec.RunMetadata{
			RunID:       "run-1",
			SubmittedAt: time.Now().UTC(),
			FailurePolicy: spec.FailurePolicy{
				Mode: "fail-fast",
			},
		},
		Graph: spec.Graph{
			Nodes: []spec.Node{{NodeID: "a", Image: "busybox:1.36"}},
		},
	}

	submitResp, err := svc.SubmitRun(context.Background(), SubmitRunRequest{Spec: specInput})
	if err != nil {
		t.Fatalf("SubmitRun() error = %v", err)
	}
	if submitResp.RunID != "run-1" {
		t.Fatalf("SubmitRun() runID = %q, want run-1", submitResp.RunID)
	}

	getResp, err := svc.GetRun(context.Background(), GetRunRequest{RunID: "run-1"})
	if err != nil {
		t.Fatalf("GetRun() error = %v", err)
	}
	if getResp.Run.Status != spec.RunStatusAdmitted {
		t.Fatalf("GetRun() status = %q, want %q", getResp.Run.Status, spec.RunStatusAdmitted)
	}

	nodesResp, err := svc.ListRunNodes(context.Background(), ListRunNodesRequest{RunID: "run-1"})
	if err != nil {
		t.Fatalf("ListRunNodes() error = %v", err)
	}
	if len(nodesResp.Nodes) != 1 {
		t.Fatalf("ListRunNodes() len = %d, want 1", len(nodesResp.Nodes))
	}
	if nodesResp.Nodes[0].Status != spec.NodeStatusPending {
		t.Fatalf("ListRunNodes() node status = %q, want %q", nodesResp.Nodes[0].Status, spec.NodeStatusPending)
	}

	now := time.Now().UTC()
	if err := reg.UpsertAttempt(context.Background(), spec.AttemptRecord{RunID: "run-1", NodeID: "a", AttemptID: "run-1-a-attempt-1", Status: spec.AttemptStatusPrepared, StartedAt: &now}); err != nil {
		t.Fatalf("UpsertAttempt() error = %v", err)
	}
	if err := reg.AppendEvent(context.Background(), spec.EventRecord{RunID: "run-1", NodeID: "a", AttemptID: "run-1-a-attempt-1", Type: "node.ready", OccurredAt: now, Level: "info"}); err != nil {
		t.Fatalf("AppendEvent() error = %v", err)
	}

	attemptsResp, err := svc.ListNodeAttempts(context.Background(), ListNodeAttemptsRequest{RunID: "run-1", NodeID: "a"})
	if err != nil {
		t.Fatalf("ListNodeAttempts() error = %v", err)
	}
	if len(attemptsResp.Attempts) != 1 {
		t.Fatalf("ListNodeAttempts() len = %d, want 1", len(attemptsResp.Attempts))
	}
	if attemptsResp.Attempts[0].AttemptID != "run-1-a-attempt-1" {
		t.Fatalf("ListNodeAttempts() attemptID = %q, want run-1-a-attempt-1", attemptsResp.Attempts[0].AttemptID)
	}

	eventsResp, err := svc.ListRunEvents(context.Background(), ListRunEventsRequest{RunID: "run-1", Limit: 10})
	if err != nil {
		t.Fatalf("ListRunEvents() error = %v", err)
	}
	if len(eventsResp.Events) == 0 {
		t.Fatal("ListRunEvents() empty, want at least one event")
	}

	cancelResp, err := svc.CancelRun(context.Background(), CancelRunRequest{RunID: "run-1", Reason: "test"})
	if err != nil {
		t.Fatalf("CancelRun() error = %v", err)
	}
	if !cancelResp.Accepted {
		t.Fatal("CancelRun() accepted = false, want true")
	}

	getResp, err = svc.GetRun(context.Background(), GetRunRequest{RunID: "run-1"})
	if err != nil {
		t.Fatalf("GetRun() after cancel error = %v", err)
	}
	if getResp.Run.Status != spec.RunStatusCanceled {
		t.Fatalf("GetRun() after cancel status = %q, want %q", getResp.Run.Status, spec.RunStatusCanceled)
	}
}
