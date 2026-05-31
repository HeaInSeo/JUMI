package registry

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/HeaInSeo/JUMI/pkg/spec"
)

func makeRun(runID string) spec.RunRecord {
	return spec.RunRecord{
		RunID:      runID,
		Status:     spec.RunStatusAccepted,
		AcceptedAt: time.Now().UTC(),
	}
}

func makeNode(runID, nodeID string) spec.NodeRecord {
	return spec.NodeRecord{
		RunID:  runID,
		NodeID: nodeID,
		Status: spec.NodeStatusPending,
	}
}

func TestMemoryRegistry_CreateAndGetRun(t *testing.T) {
	reg := NewMemoryRegistry()
	ctx := context.Background()

	run := makeRun("run-1")
	nodes := []spec.NodeRecord{makeNode("run-1", "a"), makeNode("run-1", "b")}

	if err := reg.CreateRun(ctx, run, nodes); err != nil {
		t.Fatalf("CreateRun() error = %v", err)
	}

	got, err := reg.GetRun(ctx, "run-1")
	if err != nil {
		t.Fatalf("GetRun() error = %v", err)
	}
	if got.RunID != "run-1" {
		t.Fatalf("GetRun().RunID = %q, want run-1", got.RunID)
	}
	if got.Status != spec.RunStatusAccepted {
		t.Fatalf("GetRun().Status = %q, want Accepted", got.Status)
	}
}

func TestMemoryRegistry_GetRun_NotFound(t *testing.T) {
	reg := NewMemoryRegistry()
	_, err := reg.GetRun(context.Background(), "nonexistent")
	if err == nil {
		t.Fatal("expected error for missing run")
	}
	if !errors.Is(err, ErrRunNotFound) {
		t.Fatalf("expected ErrRunNotFound, got %v", err)
	}
}

func TestMemoryRegistry_CreateRun_AlreadyExists(t *testing.T) {
	reg := NewMemoryRegistry()
	ctx := context.Background()
	run := makeRun("run-dup")
	if err := reg.CreateRun(ctx, run, nil); err != nil {
		t.Fatalf("first CreateRun() error = %v", err)
	}
	if err := reg.CreateRun(ctx, run, nil); !errors.Is(err, ErrRunAlreadyExists) {
		t.Fatalf("expected ErrRunAlreadyExists, got %v", err)
	}
}

func TestMemoryRegistry_ListRuns(t *testing.T) {
	reg := NewMemoryRegistry()
	ctx := context.Background()

	t1 := time.Now().UTC()
	t2 := t1.Add(time.Second)

	run1 := spec.RunRecord{RunID: "run-1", Status: spec.RunStatusAccepted, AcceptedAt: t1}
	run2 := spec.RunRecord{RunID: "run-2", Status: spec.RunStatusAccepted, AcceptedAt: t2}

	_ = reg.CreateRun(ctx, run2, nil)
	_ = reg.CreateRun(ctx, run1, nil)

	runs, err := reg.ListRuns(ctx)
	if err != nil {
		t.Fatalf("ListRuns() error = %v", err)
	}
	if len(runs) != 2 {
		t.Fatalf("ListRuns() len = %d, want 2", len(runs))
	}
	// Should be sorted by AcceptedAt ascending
	if runs[0].RunID != "run-1" {
		t.Fatalf("ListRuns()[0].RunID = %q, want run-1 (oldest first)", runs[0].RunID)
	}
	if runs[1].RunID != "run-2" {
		t.Fatalf("ListRuns()[1].RunID = %q, want run-2", runs[1].RunID)
	}
}

func TestMemoryRegistry_ListNodes(t *testing.T) {
	reg := NewMemoryRegistry()
	ctx := context.Background()

	run := makeRun("run-1")
	nodes := []spec.NodeRecord{makeNode("run-1", "z"), makeNode("run-1", "a"), makeNode("run-1", "m")}
	_ = reg.CreateRun(ctx, run, nodes)

	got, err := reg.ListNodes(ctx, "run-1")
	if err != nil {
		t.Fatalf("ListNodes() error = %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("ListNodes() len = %d, want 3", len(got))
	}
	// Should be sorted by NodeID ascending
	if got[0].NodeID != "a" || got[1].NodeID != "m" || got[2].NodeID != "z" {
		t.Fatalf("ListNodes() not sorted: %v", []string{got[0].NodeID, got[1].NodeID, got[2].NodeID})
	}
}

func TestMemoryRegistry_ListNodes_RunNotFound(t *testing.T) {
	reg := NewMemoryRegistry()
	_, err := reg.ListNodes(context.Background(), "nonexistent")
	if !errors.Is(err, ErrRunNotFound) {
		t.Fatalf("expected ErrRunNotFound, got %v", err)
	}
}

func TestMemoryRegistry_UpdateRun(t *testing.T) {
	reg := NewMemoryRegistry()
	ctx := context.Background()
	run := makeRun("run-1")
	_ = reg.CreateRun(ctx, run, nil)

	err := reg.UpdateRun(ctx, "run-1", func(r *spec.RunRecord) error {
		r.Status = spec.RunStatusRunning
		return nil
	})
	if err != nil {
		t.Fatalf("UpdateRun() error = %v", err)
	}

	got, _ := reg.GetRun(ctx, "run-1")
	if got.Status != spec.RunStatusRunning {
		t.Fatalf("after UpdateRun, Status = %q, want Running", got.Status)
	}
}

func TestMemoryRegistry_UpdateRun_NotFound(t *testing.T) {
	reg := NewMemoryRegistry()
	err := reg.UpdateRun(context.Background(), "nonexistent", func(r *spec.RunRecord) error {
		return nil
	})
	if !errors.Is(err, ErrRunNotFound) {
		t.Fatalf("expected ErrRunNotFound, got %v", err)
	}
}

func TestMemoryRegistry_UpdateRun_UpdateFnError(t *testing.T) {
	reg := NewMemoryRegistry()
	ctx := context.Background()
	_ = reg.CreateRun(ctx, makeRun("run-1"), nil)

	sentinel := errors.New("update failed")
	err := reg.UpdateRun(ctx, "run-1", func(r *spec.RunRecord) error {
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected sentinel error, got %v", err)
	}

	// Record should be unchanged after a failed update.
	got, _ := reg.GetRun(ctx, "run-1")
	if got.Status != spec.RunStatusAccepted {
		t.Fatalf("status changed after failed update: got %q", got.Status)
	}
}

func TestMemoryRegistry_UpdateNode(t *testing.T) {
	reg := NewMemoryRegistry()
	ctx := context.Background()
	run := makeRun("run-1")
	nodes := []spec.NodeRecord{makeNode("run-1", "a")}
	_ = reg.CreateRun(ctx, run, nodes)

	err := reg.UpdateNode(ctx, "run-1", "a", func(n *spec.NodeRecord) error {
		n.Status = spec.NodeStatusRunning
		return nil
	})
	if err != nil {
		t.Fatalf("UpdateNode() error = %v", err)
	}

	got, _ := reg.ListNodes(ctx, "run-1")
	if got[0].Status != spec.NodeStatusRunning {
		t.Fatalf("after UpdateNode, Status = %q, want Running", got[0].Status)
	}
}

func TestMemoryRegistry_UpdateNode_RunNotFound(t *testing.T) {
	reg := NewMemoryRegistry()
	err := reg.UpdateNode(context.Background(), "nonexistent", "a", func(n *spec.NodeRecord) error {
		return nil
	})
	if !errors.Is(err, ErrRunNotFound) {
		t.Fatalf("expected ErrRunNotFound, got %v", err)
	}
}

func TestMemoryRegistry_UpdateNode_NodeNotFound(t *testing.T) {
	reg := NewMemoryRegistry()
	ctx := context.Background()
	_ = reg.CreateRun(ctx, makeRun("run-1"), nil)

	err := reg.UpdateNode(ctx, "run-1", "nonexistent-node", func(n *spec.NodeRecord) error {
		return nil
	})
	if !errors.Is(err, ErrNodeNotFound) {
		t.Fatalf("expected ErrNodeNotFound, got %v", err)
	}
}

func TestMemoryRegistry_UpdateNode_UpdateFnError(t *testing.T) {
	reg := NewMemoryRegistry()
	ctx := context.Background()
	run := makeRun("run-1")
	nodes := []spec.NodeRecord{makeNode("run-1", "a")}
	_ = reg.CreateRun(ctx, run, nodes)

	sentinel := errors.New("node update failed")
	err := reg.UpdateNode(ctx, "run-1", "a", func(n *spec.NodeRecord) error {
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected sentinel error, got %v", err)
	}

	// Node should be unchanged.
	got, _ := reg.ListNodes(ctx, "run-1")
	if got[0].Status != spec.NodeStatusPending {
		t.Fatalf("status changed after failed UpdateNode: got %q", got[0].Status)
	}
}

func TestMemoryRegistry_UpsertAttempt(t *testing.T) {
	reg := NewMemoryRegistry()
	ctx := context.Background()
	run := makeRun("run-1")
	nodes := []spec.NodeRecord{makeNode("run-1", "a")}
	_ = reg.CreateRun(ctx, run, nodes)

	now := time.Now().UTC()
	attempt := spec.AttemptRecord{
		RunID:     "run-1",
		NodeID:    "a",
		AttemptID: "attempt-1",
		Status:    spec.AttemptStatusPrepared,
		StartedAt: &now,
	}
	if err := reg.UpsertAttempt(ctx, attempt); err != nil {
		t.Fatalf("UpsertAttempt() error = %v", err)
	}

	attempts, err := reg.ListAttempts(ctx, "run-1", "a")
	if err != nil {
		t.Fatalf("ListAttempts() error = %v", err)
	}
	if len(attempts) != 1 {
		t.Fatalf("ListAttempts() len = %d, want 1", len(attempts))
	}
	if attempts[0].AttemptID != "attempt-1" {
		t.Fatalf("attempt.AttemptID = %q, want attempt-1", attempts[0].AttemptID)
	}

	// Upsert again to update.
	attempt.Status = spec.AttemptStatusCompleted
	if err := reg.UpsertAttempt(ctx, attempt); err != nil {
		t.Fatalf("second UpsertAttempt() error = %v", err)
	}
	attempts, _ = reg.ListAttempts(ctx, "run-1", "a")
	if attempts[0].Status != spec.AttemptStatusCompleted {
		t.Fatalf("after upsert update, Status = %q, want Completed", attempts[0].Status)
	}
}

func TestMemoryRegistry_UpsertAttempt_RunNotFound(t *testing.T) {
	reg := NewMemoryRegistry()
	err := reg.UpsertAttempt(context.Background(), spec.AttemptRecord{
		RunID: "nonexistent", NodeID: "a", AttemptID: "x",
	})
	if !errors.Is(err, ErrRunNotFound) {
		t.Fatalf("expected ErrRunNotFound, got %v", err)
	}
}

func TestMemoryRegistry_UpsertAttempt_NodeNotFound(t *testing.T) {
	reg := NewMemoryRegistry()
	ctx := context.Background()
	_ = reg.CreateRun(ctx, makeRun("run-1"), nil) // no nodes

	err := reg.UpsertAttempt(ctx, spec.AttemptRecord{
		RunID: "run-1", NodeID: "nonexistent", AttemptID: "x",
	})
	if !errors.Is(err, ErrNodeNotFound) {
		t.Fatalf("expected ErrNodeNotFound, got %v", err)
	}
}

func TestMemoryRegistry_ListAttempts_RunNotFound(t *testing.T) {
	reg := NewMemoryRegistry()
	_, err := reg.ListAttempts(context.Background(), "nonexistent", "a")
	if !errors.Is(err, ErrRunNotFound) {
		t.Fatalf("expected ErrRunNotFound, got %v", err)
	}
}

func TestMemoryRegistry_ListAttempts_NodeNotFound(t *testing.T) {
	reg := NewMemoryRegistry()
	ctx := context.Background()
	_ = reg.CreateRun(ctx, makeRun("run-1"), nil) // no nodes

	_, err := reg.ListAttempts(ctx, "run-1", "nonexistent")
	if !errors.Is(err, ErrNodeNotFound) {
		t.Fatalf("expected ErrNodeNotFound, got %v", err)
	}
}

func TestMemoryRegistry_ListAttempts_Sorted(t *testing.T) {
	reg := NewMemoryRegistry()
	ctx := context.Background()
	run := makeRun("run-1")
	nodes := []spec.NodeRecord{makeNode("run-1", "a")}
	_ = reg.CreateRun(ctx, run, nodes)

	for _, id := range []string{"c", "a", "b"} {
		_ = reg.UpsertAttempt(ctx, spec.AttemptRecord{
			RunID: "run-1", NodeID: "a", AttemptID: id, Status: spec.AttemptStatusPrepared,
		})
	}

	attempts, err := reg.ListAttempts(ctx, "run-1", "a")
	if err != nil {
		t.Fatalf("ListAttempts() error = %v", err)
	}
	if len(attempts) != 3 {
		t.Fatalf("ListAttempts() len = %d, want 3", len(attempts))
	}
	ids := []string{attempts[0].AttemptID, attempts[1].AttemptID, attempts[2].AttemptID}
	if ids[0] != "a" || ids[1] != "b" || ids[2] != "c" {
		t.Fatalf("ListAttempts() not sorted: %v", ids)
	}
}

func TestMemoryRegistry_AppendAndListEvents(t *testing.T) {
	reg := NewMemoryRegistry()
	ctx := context.Background()
	_ = reg.CreateRun(ctx, makeRun("run-1"), nil)

	now := time.Now().UTC()
	for i := 0; i < 5; i++ {
		err := reg.AppendEvent(ctx, spec.EventRecord{
			RunID:      "run-1",
			Type:       "test.event",
			OccurredAt: now,
		})
		if err != nil {
			t.Fatalf("AppendEvent() error = %v", err)
		}
	}

	// Get all events.
	events, err := reg.ListEvents(ctx, "run-1", 0)
	if err != nil {
		t.Fatalf("ListEvents(0) error = %v", err)
	}
	if len(events) != 5 {
		t.Fatalf("ListEvents(0) len = %d, want 5", len(events))
	}

	// Get last 2 events.
	events, err = reg.ListEvents(ctx, "run-1", 2)
	if err != nil {
		t.Fatalf("ListEvents(2) error = %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("ListEvents(2) len = %d, want 2", len(events))
	}

	// Limit >= total returns all events.
	events, err = reg.ListEvents(ctx, "run-1", 100)
	if err != nil {
		t.Fatalf("ListEvents(100) error = %v", err)
	}
	if len(events) != 5 {
		t.Fatalf("ListEvents(100) len = %d, want 5", len(events))
	}
}

func TestMemoryRegistry_AppendEvent_RunNotFound(t *testing.T) {
	reg := NewMemoryRegistry()
	err := reg.AppendEvent(context.Background(), spec.EventRecord{
		RunID: "nonexistent", Type: "test", OccurredAt: time.Now(),
	})
	if !errors.Is(err, ErrRunNotFound) {
		t.Fatalf("expected ErrRunNotFound, got %v", err)
	}
}

func TestMemoryRegistry_ListEvents_RunNotFound(t *testing.T) {
	reg := NewMemoryRegistry()
	_, err := reg.ListEvents(context.Background(), "nonexistent", 0)
	if !errors.Is(err, ErrRunNotFound) {
		t.Fatalf("expected ErrRunNotFound, got %v", err)
	}
}

func TestMemoryRegistry_ListEvents_NegativeLimit(t *testing.T) {
	reg := NewMemoryRegistry()
	ctx := context.Background()
	_ = reg.CreateRun(ctx, makeRun("run-1"), nil)
	_ = reg.AppendEvent(ctx, spec.EventRecord{RunID: "run-1", Type: "t", OccurredAt: time.Now()})

	events, err := reg.ListEvents(ctx, "run-1", -1)
	if err != nil {
		t.Fatalf("ListEvents(-1) error = %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("ListEvents(-1) len = %d, want 1 (all events)", len(events))
	}
}
