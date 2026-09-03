package registry

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/HeaInSeo/JUMI/pkg/spec"
	// modernc.org/sqlite is a pure-Go (cgo-free) SQLite driver, matching the
	// driver already used by the sibling artifact-handoff service. cgo-free keeps
	// JUMI's build and tests portable without a C toolchain.
	_ "modernc.org/sqlite"
)

// SQLiteRegistry is a durable, restart-surviving implementation of Registry.
//
// It stores each Run/Node/Attempt/Event record as a JSON document, with the
// few fields needed for atomic transitions and ordered queries promoted to
// indexed columns. A single database connection serialises writes, which is
// sufficient for the F3 minimum concurrency model (a single authoritative JUMI
// writer; multi-replica leader/fencing is future work).
type SQLiteRegistry struct {
	db *sql.DB
}

// NewSQLiteRegistry opens (creating if needed) the durable registry at path.
// Use ":memory:" only for single-connection in-process use.
func NewSQLiteRegistry(path string) (*SQLiteRegistry, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	// Single connection serialises writes and makes AllocateCurrentAttempt's
	// read-check-write transaction race-free against concurrent allocators.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(30 * time.Minute)
	if err := sqliteApplyPragmas(db); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("apply pragmas: %w", err)
	}
	if err := sqliteMigrate(db); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return &SQLiteRegistry{db: db}, nil
}

func (r *SQLiteRegistry) Close() error { return r.db.Close() }

func sqliteApplyPragmas(db *sql.DB) error {
	for _, pragma := range []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA busy_timeout=5000",
		"PRAGMA synchronous=NORMAL",
		"PRAGMA foreign_keys=OFF",
	} {
		if _, err := db.Exec(pragma); err != nil {
			return fmt.Errorf("%s: %w", pragma, err)
		}
	}
	return nil
}

func sqliteMigrate(db *sql.DB) error {
	ddl := []string{
		`CREATE TABLE IF NOT EXISTS runs (
			run_id      TEXT PRIMARY KEY,
			status      TEXT NOT NULL,
			accepted_at TEXT NOT NULL,
			data        TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS nodes (
			run_id             TEXT NOT NULL,
			node_id            TEXT NOT NULL,
			status             TEXT NOT NULL,
			attempt_count      INTEGER NOT NULL DEFAULT 0,
			current_attempt_id TEXT NOT NULL DEFAULT '',
			data               TEXT NOT NULL,
			PRIMARY KEY (run_id, node_id)
		)`,
		`CREATE TABLE IF NOT EXISTS attempts (
			run_id     TEXT NOT NULL,
			node_id    TEXT NOT NULL,
			attempt_id TEXT NOT NULL,
			status     TEXT NOT NULL,
			data       TEXT NOT NULL,
			PRIMARY KEY (run_id, node_id, attempt_id)
		)`,
		`CREATE TABLE IF NOT EXISTS events (
			seq    INTEGER PRIMARY KEY AUTOINCREMENT,
			run_id TEXT NOT NULL,
			data   TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_events_run ON events (run_id, seq)`,
	}
	for _, stmt := range ddl {
		if _, err := db.Exec(stmt); err != nil {
			return fmt.Errorf("ddl: %w", err)
		}
	}
	return nil
}

func (r *SQLiteRegistry) CreateRun(ctx context.Context, record spec.RunRecord, nodes []spec.NodeRecord) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	var exists int
	if err := tx.QueryRowContext(ctx, `SELECT 1 FROM runs WHERE run_id = ?`, record.RunID).Scan(&exists); err == nil {
		return ErrRunAlreadyExists
	} else if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	runData, err := json.Marshal(record)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO runs (run_id, status, accepted_at, data) VALUES (?, ?, ?, ?)`,
		record.RunID, string(record.Status), timeText(record.AcceptedAt), string(runData)); err != nil {
		return err
	}
	for _, node := range nodes {
		nodeData, err := json.Marshal(node)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO nodes (run_id, node_id, status, attempt_count, current_attempt_id, data)
			 VALUES (?, ?, ?, ?, ?, ?)`,
			node.RunID, node.NodeID, string(node.Status), node.AttemptCount, node.CurrentAttemptID, string(nodeData)); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (r *SQLiteRegistry) GetRun(ctx context.Context, runID string) (spec.RunRecord, error) {
	var data string
	err := r.db.QueryRowContext(ctx, `SELECT data FROM runs WHERE run_id = ?`, runID).Scan(&data)
	if errors.Is(err, sql.ErrNoRows) {
		return spec.RunRecord{}, ErrRunNotFound
	}
	if err != nil {
		return spec.RunRecord{}, err
	}
	var record spec.RunRecord
	if err := json.Unmarshal([]byte(data), &record); err != nil {
		return spec.RunRecord{}, err
	}
	return record, nil
}

func (r *SQLiteRegistry) ListRuns(ctx context.Context) ([]spec.RunRecord, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT data FROM runs ORDER BY accepted_at ASC, run_id ASC`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []spec.RunRecord
	for rows.Next() {
		var data string
		if err := rows.Scan(&data); err != nil {
			return nil, err
		}
		var record spec.RunRecord
		if err := json.Unmarshal([]byte(data), &record); err != nil {
			return nil, err
		}
		out = append(out, record)
	}
	return out, rows.Err()
}

func (r *SQLiteRegistry) GetNode(ctx context.Context, runID, nodeID string) (spec.NodeRecord, error) {
	return getNodeTx(ctx, r.db, runID, nodeID)
}

func (r *SQLiteRegistry) ListNodes(ctx context.Context, runID string) ([]spec.NodeRecord, error) {
	if err := r.assertRunExists(ctx, runID); err != nil {
		return nil, err
	}
	rows, err := r.db.QueryContext(ctx, `SELECT data FROM nodes WHERE run_id = ? ORDER BY node_id ASC`, runID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []spec.NodeRecord
	for rows.Next() {
		var data string
		if err := rows.Scan(&data); err != nil {
			return nil, err
		}
		var record spec.NodeRecord
		if err := json.Unmarshal([]byte(data), &record); err != nil {
			return nil, err
		}
		out = append(out, record)
	}
	return out, rows.Err()
}

func (r *SQLiteRegistry) ListAttempts(ctx context.Context, runID, nodeID string) ([]spec.AttemptRecord, error) {
	if err := r.assertRunExists(ctx, runID); err != nil {
		return nil, err
	}
	if err := r.assertNodeExists(ctx, runID, nodeID); err != nil {
		return nil, err
	}
	rows, err := r.db.QueryContext(ctx, `SELECT data FROM attempts WHERE run_id = ? AND node_id = ? ORDER BY attempt_id ASC`, runID, nodeID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []spec.AttemptRecord
	for rows.Next() {
		var data string
		if err := rows.Scan(&data); err != nil {
			return nil, err
		}
		var record spec.AttemptRecord
		if err := json.Unmarshal([]byte(data), &record); err != nil {
			return nil, err
		}
		out = append(out, record)
	}
	return out, rows.Err()
}

func (r *SQLiteRegistry) ListEvents(ctx context.Context, runID string, limit int) ([]spec.EventRecord, error) {
	if err := r.assertRunExists(ctx, runID); err != nil {
		return nil, err
	}
	rows, err := r.db.QueryContext(ctx, `SELECT data FROM events WHERE run_id = ? ORDER BY seq ASC`, runID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var all []spec.EventRecord
	for rows.Next() {
		var data string
		if err := rows.Scan(&data); err != nil {
			return nil, err
		}
		var record spec.EventRecord
		if err := json.Unmarshal([]byte(data), &record); err != nil {
			return nil, err
		}
		all = append(all, record)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if limit <= 0 || limit >= len(all) {
		return all, nil
	}
	return all[len(all)-limit:], nil
}

func (r *SQLiteRegistry) UpdateRun(ctx context.Context, runID string, update func(*spec.RunRecord) error) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	var data string
	err = tx.QueryRowContext(ctx, `SELECT data FROM runs WHERE run_id = ?`, runID).Scan(&data)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrRunNotFound
	}
	if err != nil {
		return err
	}
	var record spec.RunRecord
	if err := json.Unmarshal([]byte(data), &record); err != nil {
		return err
	}
	if err := update(&record); err != nil {
		return err
	}
	updated, err := json.Marshal(record)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE runs SET status = ?, accepted_at = ?, data = ? WHERE run_id = ?`,
		string(record.Status), timeText(record.AcceptedAt), string(updated), runID); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *SQLiteRegistry) UpdateNode(ctx context.Context, runID, nodeID string, update func(*spec.NodeRecord) error) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	node, err := getNodeTx(ctx, tx, runID, nodeID)
	if err != nil {
		return err
	}
	if err := update(&node); err != nil {
		return err
	}
	if err := writeNodeTx(ctx, tx, node); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *SQLiteRegistry) UpsertAttempt(ctx context.Context, record spec.AttemptRecord) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if err := assertRunExistsTx(ctx, tx, record.RunID); err != nil {
		return err
	}
	if err := assertNodeExistsTx(ctx, tx, record.RunID, record.NodeID); err != nil {
		return err
	}
	if err := writeAttemptTx(ctx, tx, record); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *SQLiteRegistry) AppendEvent(ctx context.Context, event spec.EventRecord) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if err := assertRunExistsTx(ctx, tx, event.RunID); err != nil {
		return err
	}
	data, err := json.Marshal(event)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO events (run_id, data) VALUES (?, ?)`, event.RunID, string(data)); err != nil {
		return err
	}
	return tx.Commit()
}

// --- F3 durable execution-truth operations ---

func (r *SQLiteRegistry) GetCurrentAttempt(ctx context.Context, runID, nodeID string) (spec.AttemptRecord, bool, error) {
	node, err := getNodeTx(ctx, r.db, runID, nodeID)
	if err != nil {
		return spec.AttemptRecord{}, false, err
	}
	if node.CurrentAttemptID == "" {
		return spec.AttemptRecord{}, false, nil
	}
	attempt, ok, err := getAttemptTx(ctx, r.db, runID, nodeID, node.CurrentAttemptID)
	if err != nil {
		return spec.AttemptRecord{}, false, err
	}
	return attempt, ok, nil
}

func (r *SQLiteRegistry) AllocateCurrentAttempt(ctx context.Context, runID, nodeID string) (spec.AttemptRecord, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return spec.AttemptRecord{}, err
	}
	defer func() { _ = tx.Rollback() }()

	node, err := getNodeTx(ctx, tx, runID, nodeID)
	if err != nil {
		return spec.AttemptRecord{}, err
	}
	if node.CurrentAttemptID != "" {
		cur, ok, err := getAttemptTx(ctx, tx, runID, nodeID, node.CurrentAttemptID)
		if err != nil {
			return spec.AttemptRecord{}, err
		}
		if ok && !cur.Status.IsTerminal() {
			return spec.AttemptRecord{}, ErrAttemptNonTerminal
		}
	}
	// F3-B3: allocate a REALIZATION cycle (pre-user-code preparation), not a
	// user-code execution opportunity. Increment the separate RealizationAttemptCount
	// and derive the id from it; AttemptCount (the MaxAttempts opportunity budget) is
	// consumed only when the semantic Attempt opens at the fence (OpenSemanticAttempt).
	next := node.RealizationAttemptCount + 1
	attemptID := spec.DeterministicAttemptID(runID, nodeID, next)
	now := time.Now().UTC()
	attempt := spec.AttemptRecord{
		RunID:     runID,
		NodeID:    nodeID,
		AttemptID: attemptID,
		Status:    spec.AttemptStatusPrepared,
		StartedAt: &now,
	}
	if err := writeAttemptTx(ctx, tx, attempt); err != nil {
		return spec.AttemptRecord{}, err
	}
	node.RealizationAttemptCount = next
	node.CurrentAttemptID = attemptID
	node.Status = spec.NodeStatusReady
	node.CurrentBottleneckLocation = "release_wait"
	node.StartedAt = &now
	if err := writeNodeTx(ctx, tx, node); err != nil {
		return spec.AttemptRecord{}, err
	}
	if err := tx.Commit(); err != nil {
		return spec.AttemptRecord{}, err
	}
	return attempt, nil
}

// OpenSemanticAttempt records that the current reservation's submission fence was
// crossed — the semantic Attempt opens and consumes one user-code execution
// opportunity (AttemptCount++). Callers MUST verify a slot is available
// (AttemptCount < RetryPolicy.MaxAttempts) BEFORE calling; this applies the atomic
// AttemptCount increment + fence timestamp on the current attempt in one tx.
func (r *SQLiteRegistry) OpenSemanticAttempt(ctx context.Context, runID, nodeID, attemptID string, openedAt time.Time) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	node, err := getNodeTx(ctx, tx, runID, nodeID)
	if err != nil {
		return err
	}
	if node.CurrentAttemptID != attemptID {
		return ErrAttemptNotFound
	}
	attempt, ok, err := getAttemptTx(ctx, tx, runID, nodeID, attemptID)
	if err != nil {
		return err
	}
	if !ok {
		return ErrAttemptNotFound
	}
	t := openedAt.UTC()
	attempt.SubmissionWindowOpenedAt = &t
	if err := writeAttemptTx(ctx, tx, attempt); err != nil {
		return err
	}
	node.AttemptCount++
	if err := writeNodeTx(ctx, tx, node); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *SQLiteRegistry) PersistSubmissionFence(ctx context.Context, runID, nodeID, attemptID string, openedAt time.Time) error {
	return r.mutateAttempt(ctx, runID, nodeID, attemptID, func(a *spec.AttemptRecord) {
		t := openedAt.UTC()
		a.SubmissionWindowOpenedAt = &t
	})
}

func (r *SQLiteRegistry) PersistBackendHandle(ctx context.Context, runID, nodeID, attemptID, handleJSON string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	attempt, ok, err := getAttemptTx(ctx, tx, runID, nodeID, attemptID)
	if err != nil {
		return err
	}
	if !ok {
		return ErrAttemptNotFound
	}
	attempt.BackendHandleJSON = handleJSON
	if err := writeAttemptTx(ctx, tx, attempt); err != nil {
		return err
	}
	// Compat projection: mirror the current Attempt's handle onto the node.
	node, err := getNodeTx(ctx, tx, runID, nodeID)
	if err != nil {
		return err
	}
	if node.CurrentAttemptID == attemptID {
		node.CurrentAttemptHandleJSON = handleJSON
		if err := writeNodeTx(ctx, tx, node); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (r *SQLiteRegistry) PersistCancellationIntent(ctx context.Context, runID, nodeID, attemptID string, requestedAt time.Time, reason string) error {
	return r.mutateAttempt(ctx, runID, nodeID, attemptID, func(a *spec.AttemptRecord) {
		t := requestedAt.UTC()
		a.CancellationRequestedAt = &t
		a.CancellationReason = reason
	})
}

func (r *SQLiteRegistry) PersistProcessCompleted(ctx context.Context, runID, nodeID, attemptID string, completedAt time.Time) error {
	return r.mutateAttempt(ctx, runID, nodeID, attemptID, func(a *spec.AttemptRecord) {
		t := completedAt.UTC()
		a.ProcessCompletedAt = &t
	})
}

func (r *SQLiteRegistry) mutateAttempt(ctx context.Context, runID, nodeID, attemptID string, mutate func(*spec.AttemptRecord)) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	attempt, ok, err := getAttemptTx(ctx, tx, runID, nodeID, attemptID)
	if err != nil {
		return err
	}
	if !ok {
		return ErrAttemptNotFound
	}
	mutate(&attempt)
	if err := writeAttemptTx(ctx, tx, attempt); err != nil {
		return err
	}
	return tx.Commit()
}

// --- shared helpers over *sql.DB or *sql.Tx ---

type querier interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

type execer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

func (r *SQLiteRegistry) assertRunExists(ctx context.Context, runID string) error {
	return assertRunExistsTx(ctx, r.db, runID)
}

func (r *SQLiteRegistry) assertNodeExists(ctx context.Context, runID, nodeID string) error {
	return assertNodeExistsTx(ctx, r.db, runID, nodeID)
}

func assertRunExistsTx(ctx context.Context, q querier, runID string) error {
	var x int
	err := q.QueryRowContext(ctx, `SELECT 1 FROM runs WHERE run_id = ?`, runID).Scan(&x)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrRunNotFound
	}
	return err
}

func assertNodeExistsTx(ctx context.Context, q querier, runID, nodeID string) error {
	var x int
	err := q.QueryRowContext(ctx, `SELECT 1 FROM nodes WHERE run_id = ? AND node_id = ?`, runID, nodeID).Scan(&x)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNodeNotFound
	}
	return err
}

func getNodeTx(ctx context.Context, q querier, runID, nodeID string) (spec.NodeRecord, error) {
	var data string
	err := q.QueryRowContext(ctx, `SELECT data FROM nodes WHERE run_id = ? AND node_id = ?`, runID, nodeID).Scan(&data)
	if errors.Is(err, sql.ErrNoRows) {
		// Distinguish missing run from missing node to match MemoryRegistry.
		if rerr := assertRunExistsTx(ctx, q, runID); rerr != nil {
			return spec.NodeRecord{}, rerr
		}
		return spec.NodeRecord{}, ErrNodeNotFound
	}
	if err != nil {
		return spec.NodeRecord{}, err
	}
	var node spec.NodeRecord
	if err := json.Unmarshal([]byte(data), &node); err != nil {
		return spec.NodeRecord{}, err
	}
	return node, nil
}

func getAttemptTx(ctx context.Context, q querier, runID, nodeID, attemptID string) (spec.AttemptRecord, bool, error) {
	var data string
	err := q.QueryRowContext(ctx, `SELECT data FROM attempts WHERE run_id = ? AND node_id = ? AND attempt_id = ?`, runID, nodeID, attemptID).Scan(&data)
	if errors.Is(err, sql.ErrNoRows) {
		return spec.AttemptRecord{}, false, nil
	}
	if err != nil {
		return spec.AttemptRecord{}, false, err
	}
	var attempt spec.AttemptRecord
	if err := json.Unmarshal([]byte(data), &attempt); err != nil {
		return spec.AttemptRecord{}, false, err
	}
	return attempt, true, nil
}

func writeNodeTx(ctx context.Context, e execer, node spec.NodeRecord) error {
	data, err := json.Marshal(node)
	if err != nil {
		return err
	}
	_, err = e.ExecContext(ctx,
		`UPDATE nodes SET status = ?, attempt_count = ?, current_attempt_id = ?, data = ? WHERE run_id = ? AND node_id = ?`,
		string(node.Status), node.AttemptCount, node.CurrentAttemptID, string(data), node.RunID, node.NodeID)
	return err
}

func writeAttemptTx(ctx context.Context, e execer, attempt spec.AttemptRecord) error {
	data, err := json.Marshal(attempt)
	if err != nil {
		return err
	}
	_, err = e.ExecContext(ctx,
		`INSERT INTO attempts (run_id, node_id, attempt_id, status, data) VALUES (?, ?, ?, ?, ?)
		 ON CONFLICT (run_id, node_id, attempt_id) DO UPDATE SET status = excluded.status, data = excluded.data`,
		attempt.RunID, attempt.NodeID, attempt.AttemptID, string(attempt.Status), string(data))
	return err
}

func timeText(t time.Time) string {
	return t.UTC().Format(time.RFC3339Nano)
}
