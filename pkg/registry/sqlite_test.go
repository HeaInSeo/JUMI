package registry

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/HeaInSeo/JUMI/pkg/spec"
)

func newTempSQLite(t *testing.T) *SQLiteRegistry {
	t.Helper()
	path := filepath.Join(t.TempDir(), "registry.db")
	reg, err := NewSQLiteRegistry(path)
	if err != nil {
		t.Fatalf("NewSQLiteRegistry() error = %v", err)
	}
	t.Cleanup(func() { _ = reg.Close() })
	return reg
}

func seedRun(t *testing.T, reg Registry, runID string, nodeIDs ...string) {
	t.Helper()
	nodes := make([]spec.NodeRecord, 0, len(nodeIDs))
	for _, id := range nodeIDs {
		nodes = append(nodes, spec.NodeRecord{RunID: runID, NodeID: id, Status: spec.NodeStatusPending})
	}
	run := spec.RunRecord{RunID: runID, Status: spec.RunStatusAccepted, AcceptedAt: time.Now().UTC()}
	if err := reg.CreateRun(context.Background(), run, nodes); err != nil {
		t.Fatalf("CreateRun() error = %v", err)
	}
}

// Basic conformance: the SQLite registry behaves like MemoryRegistry for the
// shared interface.
func TestSQLiteRegistry_CreateGetRun(t *testing.T) {
	reg := newTempSQLite(t)
	ctx := context.Background()
	seedRun(t, reg, "run-1", "a", "b")

	got, err := reg.GetRun(ctx, "run-1")
	if err != nil {
		t.Fatalf("GetRun() error = %v", err)
	}
	if got.RunID != "run-1" || got.Status != spec.RunStatusAccepted {
		t.Fatalf("GetRun() = %+v", got)
	}
	if _, err := reg.GetRun(ctx, "missing"); !errors.Is(err, ErrRunNotFound) {
		t.Fatalf("expected ErrRunNotFound, got %v", err)
	}
	nodes, err := reg.ListNodes(ctx, "run-1")
	if err != nil || len(nodes) != 2 {
		t.Fatalf("ListNodes() = %v, err = %v", nodes, err)
	}
	if nodes[0].NodeID != "a" || nodes[1].NodeID != "b" {
		t.Fatalf("ListNodes() not sorted: %v", nodes)
	}
}

func TestSQLiteRegistry_CreateRun_AlreadyExists(t *testing.T) {
	reg := newTempSQLite(t)
	seedRun(t, reg, "dup")
	err := reg.CreateRun(context.Background(), spec.RunRecord{RunID: "dup", Status: spec.RunStatusAccepted, AcceptedAt: time.Now().UTC()}, nil)
	if !errors.Is(err, ErrRunAlreadyExists) {
		t.Fatalf("expected ErrRunAlreadyExists, got %v", err)
	}
}

func TestSQLiteRegistry_UpdateNodeAndAttempts(t *testing.T) {
	reg := newTempSQLite(t)
	ctx := context.Background()
	seedRun(t, reg, "run-1", "a")

	if err := reg.UpdateNode(ctx, "run-1", "a", func(n *spec.NodeRecord) error {
		n.Status = spec.NodeStatusRunning
		return nil
	}); err != nil {
		t.Fatalf("UpdateNode() error = %v", err)
	}
	node, _ := reg.GetNode(ctx, "run-1", "a")
	if node.Status != spec.NodeStatusRunning {
		t.Fatalf("node status = %q", node.Status)
	}

	now := time.Now().UTC()
	if err := reg.UpsertAttempt(ctx, spec.AttemptRecord{RunID: "run-1", NodeID: "a", AttemptID: "x", Status: spec.AttemptStatusPrepared, StartedAt: &now}); err != nil {
		t.Fatalf("UpsertAttempt() error = %v", err)
	}
	// Upsert update path.
	if err := reg.UpsertAttempt(ctx, spec.AttemptRecord{RunID: "run-1", NodeID: "a", AttemptID: "x", Status: spec.AttemptStatusCompleted}); err != nil {
		t.Fatalf("UpsertAttempt() update error = %v", err)
	}
	attempts, _ := reg.ListAttempts(ctx, "run-1", "a")
	if len(attempts) != 1 || attempts[0].Status != spec.AttemptStatusCompleted {
		t.Fatalf("ListAttempts() = %+v", attempts)
	}
}

func TestSQLiteRegistry_Events(t *testing.T) {
	reg := newTempSQLite(t)
	ctx := context.Background()
	seedRun(t, reg, "run-1")
	for i := 0; i < 5; i++ {
		if err := reg.AppendEvent(ctx, spec.EventRecord{RunID: "run-1", Type: "t", OccurredAt: time.Now().UTC()}); err != nil {
			t.Fatalf("AppendEvent() error = %v", err)
		}
	}
	all, _ := reg.ListEvents(ctx, "run-1", 0)
	if len(all) != 5 {
		t.Fatalf("ListEvents(0) len = %d", len(all))
	}
	last2, _ := reg.ListEvents(ctx, "run-1", 2)
	if len(last2) != 2 {
		t.Fatalf("ListEvents(2) len = %d", len(last2))
	}
}

// F3-T12: concurrent next-attempt allocation → exactly one commits.
func TestSQLiteRegistry_F3T12_AllocateCurrentAttempt_ExactlyOnce(t *testing.T) {
	reg := newTempSQLite(t)
	ctx := context.Background()
	seedRun(t, reg, "run-1", "a")

	const workers = 8
	var wg sync.WaitGroup
	var mu sync.Mutex
	var successes []spec.AttemptRecord
	var nonTerminalRejects int
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()
			att, err := reg.AllocateCurrentAttempt(ctx, "run-1", "a")
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				successes = append(successes, att)
			case errors.Is(err, ErrAttemptNonTerminal):
				nonTerminalRejects++
			default:
				t.Errorf("unexpected error: %v", err)
			}
		}()
	}
	wg.Wait()

	if len(successes) != 1 {
		t.Fatalf("expected exactly one successful allocation, got %d", len(successes))
	}
	if nonTerminalRejects != workers-1 {
		t.Fatalf("expected %d ErrAttemptNonTerminal rejects, got %d", workers-1, nonTerminalRejects)
	}
	want := spec.DeterministicAttemptID("run-1", "a", 1)
	if successes[0].AttemptID != want {
		t.Fatalf("attemptID = %q, want %q", successes[0].AttemptID, want)
	}
	// Node counter reflects exactly one allocation; only one attempt row exists.
	// F3-B3: a realization allocation increments RealizationAttemptCount, not the
	// user-code opportunity budget (AttemptCount, consumed only at fence open).
	node, _ := reg.GetNode(ctx, "run-1", "a")
	if node.RealizationAttemptCount != 1 || node.AttemptCount != 0 || node.CurrentAttemptID != want {
		t.Fatalf("node = {realization:%d count:%d current:%q}, want {1 0 %q}", node.RealizationAttemptCount, node.AttemptCount, node.CurrentAttemptID, want)
	}
	attempts, _ := reg.ListAttempts(ctx, "run-1", "a")
	if len(attempts) != 1 {
		t.Fatalf("expected 1 attempt row, got %d", len(attempts))
	}
}

// AllocateCurrentAttempt permits a fresh allocation once the current Attempt is
// terminal (legitimate retry after terminal truth), and forbids it while the
// current Attempt is non-terminal.
func TestSQLiteRegistry_AllocateCurrentAttempt_RetryAfterTerminal(t *testing.T) {
	reg := newTempSQLite(t)
	ctx := context.Background()
	seedRun(t, reg, "run-1", "a")

	a1, err := reg.AllocateCurrentAttempt(ctx, "run-1", "a")
	if err != nil {
		t.Fatalf("first allocate error = %v", err)
	}
	// While non-terminal → reject.
	if _, err := reg.AllocateCurrentAttempt(ctx, "run-1", "a"); !errors.Is(err, ErrAttemptNonTerminal) {
		t.Fatalf("expected ErrAttemptNonTerminal, got %v", err)
	}
	// Terminalize a1, then a new allocation is allowed.
	if err := reg.UpsertAttempt(ctx, spec.AttemptRecord{RunID: "run-1", NodeID: "a", AttemptID: a1.AttemptID, Status: spec.AttemptStatusErrored}); err != nil {
		t.Fatalf("terminalize error = %v", err)
	}
	a2, err := reg.AllocateCurrentAttempt(ctx, "run-1", "a")
	if err != nil {
		t.Fatalf("second allocate error = %v", err)
	}
	if a2.AttemptID != spec.DeterministicAttemptID("run-1", "a", 2) {
		t.Fatalf("a2 = %q, want attempt-2", a2.AttemptID)
	}
}

// F3-T01: response lost + restart → same Run recoverable, no duplicate.
// Reopening the same database file recovers the same Run/Node/Attempt truth.
func TestSQLiteRegistry_F3T01_DurableAcrossRestart(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "registry.db")

	reg1, err := NewSQLiteRegistry(path)
	if err != nil {
		t.Fatalf("open reg1 error = %v", err)
	}
	seedRun(t, reg1, "run-1", "a")
	att, err := reg1.AllocateCurrentAttempt(ctx, "run-1", "a")
	if err != nil {
		t.Fatalf("allocate error = %v", err)
	}
	if err := reg1.Close(); err != nil {
		t.Fatalf("close reg1 error = %v", err)
	}

	// Simulate process restart: reopen the same file.
	reg2, err := NewSQLiteRegistry(path)
	if err != nil {
		t.Fatalf("open reg2 error = %v", err)
	}
	defer func() { _ = reg2.Close() }()

	got, err := reg2.GetRun(ctx, "run-1")
	if err != nil {
		t.Fatalf("recovered GetRun error = %v", err)
	}
	if got.RunID != "run-1" {
		t.Fatalf("recovered run = %+v", got)
	}
	cur, ok, err := reg2.GetCurrentAttempt(ctx, "run-1", "a")
	if err != nil || !ok {
		t.Fatalf("recovered GetCurrentAttempt ok = %v err = %v", ok, err)
	}
	if cur.AttemptID != att.AttemptID {
		t.Fatalf("recovered attempt = %q, want %q (same, no duplicate)", cur.AttemptID, att.AttemptID)
	}
	// No duplicate allocation happened: still exactly one attempt.
	// F3-B3: the durable realization counter is 1; the user-code opportunity budget
	// (AttemptCount) is untouched until the fence opens.
	node, _ := reg2.GetNode(ctx, "run-1", "a")
	if node.RealizationAttemptCount != 1 {
		t.Fatalf("recovered RealizationAttemptCount = %d, want 1", node.RealizationAttemptCount)
	}
	attempts, _ := reg2.ListAttempts(ctx, "run-1", "a")
	if len(attempts) != 1 {
		t.Fatalf("recovered attempt count = %d, want 1", len(attempts))
	}
}

// F3-T08: cancel accepted + restart → intent recovered.
func TestSQLiteRegistry_F3T08_CancellationIntentDurable(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "registry.db")

	reg1, err := NewSQLiteRegistry(path)
	if err != nil {
		t.Fatalf("open reg1 error = %v", err)
	}
	seedRun(t, reg1, "run-1", "a")
	att, err := reg1.AllocateCurrentAttempt(ctx, "run-1", "a")
	if err != nil {
		t.Fatalf("allocate error = %v", err)
	}
	reqAt := time.Now().UTC().Truncate(time.Millisecond)
	if err := reg1.PersistCancellationIntent(ctx, "run-1", "a", att.AttemptID, reqAt, "user_requested"); err != nil {
		t.Fatalf("PersistCancellationIntent error = %v", err)
	}
	_ = reg1.Close()

	reg2, err := NewSQLiteRegistry(path)
	if err != nil {
		t.Fatalf("open reg2 error = %v", err)
	}
	defer func() { _ = reg2.Close() }()

	cur, ok, err := reg2.GetCurrentAttempt(ctx, "run-1", "a")
	if err != nil || !ok {
		t.Fatalf("GetCurrentAttempt ok = %v err = %v", ok, err)
	}
	if cur.CancellationRequestedAt == nil {
		t.Fatalf("cancellation intent not recovered")
	}
	if !cur.CancellationRequestedAt.Equal(reqAt) {
		t.Fatalf("recovered CancellationRequestedAt = %v, want %v", cur.CancellationRequestedAt, reqAt)
	}
	if cur.CancellationReason != "user_requested" {
		t.Fatalf("recovered CancellationReason = %q", cur.CancellationReason)
	}
}

func TestSQLiteRegistry_SubmissionFenceAndHandleDurable(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "registry.db")
	reg1, err := NewSQLiteRegistry(path)
	if err != nil {
		t.Fatalf("open error = %v", err)
	}
	seedRun(t, reg1, "run-1", "a")
	att, _ := reg1.AllocateCurrentAttempt(ctx, "run-1", "a")
	fenceAt := time.Now().UTC().Truncate(time.Millisecond)
	if err := reg1.PersistSubmissionFence(ctx, "run-1", "a", att.AttemptID, fenceAt); err != nil {
		t.Fatalf("PersistSubmissionFence error = %v", err)
	}
	if err := reg1.PersistBackendHandle(ctx, "run-1", "a", att.AttemptID, `{"job":"j1"}`); err != nil {
		t.Fatalf("PersistBackendHandle error = %v", err)
	}
	_ = reg1.Close()

	reg2, err := NewSQLiteRegistry(path)
	if err != nil {
		t.Fatalf("reopen error = %v", err)
	}
	defer func() { _ = reg2.Close() }()
	cur, ok, _ := reg2.GetCurrentAttempt(ctx, "run-1", "a")
	if !ok || cur.SubmissionWindowOpenedAt == nil {
		t.Fatalf("submission fence not recovered: %+v", cur)
	}
	if cur.BackendHandleJSON != `{"job":"j1"}` {
		t.Fatalf("handle not recovered on attempt: %q", cur.BackendHandleJSON)
	}
	// Compat projection: node-level handle mirrors the attempt handle.
	node, _ := reg2.GetNode(ctx, "run-1", "a")
	if node.CurrentAttemptHandleJSON != `{"job":"j1"}` {
		t.Fatalf("handle not mirrored to node: %q", node.CurrentAttemptHandleJSON)
	}
}

func TestSQLiteRegistry_DurableOps_NotFound(t *testing.T) {
	reg := newTempSQLite(t)
	ctx := context.Background()
	seedRun(t, reg, "run-1", "a")
	if _, err := reg.AllocateCurrentAttempt(ctx, "run-1", "missing"); !errors.Is(err, ErrNodeNotFound) {
		t.Fatalf("expected ErrNodeNotFound, got %v", err)
	}
	if err := reg.PersistSubmissionFence(ctx, "run-1", "a", "no-attempt", time.Now()); !errors.Is(err, ErrAttemptNotFound) {
		t.Fatalf("expected ErrAttemptNotFound, got %v", err)
	}
	_, ok, err := reg.GetCurrentAttempt(ctx, "run-1", "a")
	if err != nil || ok {
		t.Fatalf("GetCurrentAttempt on node with no attempt: ok = %v err = %v", ok, err)
	}
}
