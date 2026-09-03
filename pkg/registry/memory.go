package registry

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/HeaInSeo/JUMI/pkg/spec"
)

var ErrRunNotFound = errors.New("run not found")
var ErrNodeNotFound = errors.New("node not found")
var ErrRunAlreadyExists = errors.New("run already exists")
var ErrAttemptNotFound = errors.New("attempt not found")

// ErrAttemptNonTerminal is returned by AllocateCurrentAttempt when the node's
// current Attempt exists and has not reached terminal truth. It enforces the
// F3 invariant that no replacement (semantic) Attempt is created while the
// current Attempt's backend truth is unresolved.
var ErrAttemptNonTerminal = errors.New("current attempt is non-terminal")

type MemoryRegistry struct {
	mu       sync.RWMutex
	runs     map[string]spec.RunRecord
	nodes    map[string]map[string]spec.NodeRecord
	attempts map[string]map[string]map[string]spec.AttemptRecord
	events   map[string][]spec.EventRecord
}

func NewMemoryRegistry() *MemoryRegistry {
	return &MemoryRegistry{
		runs:     make(map[string]spec.RunRecord),
		nodes:    make(map[string]map[string]spec.NodeRecord),
		attempts: make(map[string]map[string]map[string]spec.AttemptRecord),
		events:   make(map[string][]spec.EventRecord),
	}
}

func (r *MemoryRegistry) CreateRun(_ context.Context, record spec.RunRecord, nodes []spec.NodeRecord) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.runs[record.RunID]; exists {
		return ErrRunAlreadyExists
	}
	r.runs[record.RunID] = record
	r.nodes[record.RunID] = make(map[string]spec.NodeRecord, len(nodes))
	r.attempts[record.RunID] = make(map[string]map[string]spec.AttemptRecord, len(nodes))
	for _, node := range nodes {
		r.nodes[record.RunID][node.NodeID] = node
		r.attempts[record.RunID][node.NodeID] = make(map[string]spec.AttemptRecord)
	}
	r.events[record.RunID] = nil
	return nil
}

func (r *MemoryRegistry) GetRun(_ context.Context, runID string) (spec.RunRecord, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	record, ok := r.runs[runID]
	if !ok {
		return spec.RunRecord{}, ErrRunNotFound
	}
	return record, nil
}

func (r *MemoryRegistry) ListRuns(_ context.Context) ([]spec.RunRecord, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]spec.RunRecord, 0, len(r.runs))
	for _, run := range r.runs {
		out = append(out, run)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].AcceptedAt.Before(out[j].AcceptedAt) })
	return out, nil
}

func (r *MemoryRegistry) GetNode(_ context.Context, runID, nodeID string) (spec.NodeRecord, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	nodes, ok := r.nodes[runID]
	if !ok {
		return spec.NodeRecord{}, ErrRunNotFound
	}
	if node, ok := nodes[nodeID]; ok {
		return node, nil
	}
	return spec.NodeRecord{}, fmt.Errorf("node not found: %s/%s", runID, nodeID)
}

func (r *MemoryRegistry) ListNodes(_ context.Context, runID string) ([]spec.NodeRecord, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	nodes, ok := r.nodes[runID]
	if !ok {
		return nil, ErrRunNotFound
	}
	out := make([]spec.NodeRecord, 0, len(nodes))
	for _, node := range nodes {
		out = append(out, node)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].NodeID < out[j].NodeID })
	return out, nil
}

func (r *MemoryRegistry) ListAttempts(_ context.Context, runID, nodeID string) ([]spec.AttemptRecord, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	runAttempts, ok := r.attempts[runID]
	if !ok {
		return nil, ErrRunNotFound
	}
	nodeAttempts, ok := runAttempts[nodeID]
	if !ok {
		return nil, ErrNodeNotFound
	}
	out := make([]spec.AttemptRecord, 0, len(nodeAttempts))
	for _, attempt := range nodeAttempts {
		out = append(out, attempt)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].AttemptID < out[j].AttemptID })
	return out, nil
}

func (r *MemoryRegistry) ListEvents(_ context.Context, runID string, limit int) ([]spec.EventRecord, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	events, ok := r.events[runID]
	if !ok {
		return nil, ErrRunNotFound
	}
	if limit <= 0 || limit >= len(events) {
		out := make([]spec.EventRecord, len(events))
		copy(out, events)
		return out, nil
	}
	start := len(events) - limit
	out := make([]spec.EventRecord, len(events[start:]))
	copy(out, events[start:])
	return out, nil
}

func (r *MemoryRegistry) UpdateRun(_ context.Context, runID string, update func(*spec.RunRecord) error) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	record, ok := r.runs[runID]
	if !ok {
		return ErrRunNotFound
	}
	if err := update(&record); err != nil {
		return err
	}
	r.runs[runID] = record
	return nil
}

func (r *MemoryRegistry) UpdateNode(_ context.Context, runID, nodeID string, update func(*spec.NodeRecord) error) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	runNodes, ok := r.nodes[runID]
	if !ok {
		return ErrRunNotFound
	}
	node, ok := runNodes[nodeID]
	if !ok {
		return ErrNodeNotFound
	}
	if err := update(&node); err != nil {
		return err
	}
	runNodes[nodeID] = node
	return nil
}

func (r *MemoryRegistry) UpsertAttempt(_ context.Context, record spec.AttemptRecord) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	runAttempts, ok := r.attempts[record.RunID]
	if !ok {
		return ErrRunNotFound
	}
	nodeAttempts, ok := runAttempts[record.NodeID]
	if !ok {
		return ErrNodeNotFound
	}
	nodeAttempts[record.AttemptID] = record
	return nil
}

func (r *MemoryRegistry) AppendEvent(_ context.Context, event spec.EventRecord) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.runs[event.RunID]; !ok {
		return ErrRunNotFound
	}
	r.events[event.RunID] = append(r.events[event.RunID], event)
	return nil
}

// --- F3 durable execution-truth operations ---

func (r *MemoryRegistry) GetCurrentAttempt(_ context.Context, runID, nodeID string) (spec.AttemptRecord, bool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	node, err := r.lockedGetNode(runID, nodeID)
	if err != nil {
		return spec.AttemptRecord{}, false, err
	}
	if node.CurrentAttemptID == "" {
		return spec.AttemptRecord{}, false, nil
	}
	attempt, ok := r.attempts[runID][nodeID][node.CurrentAttemptID]
	if !ok {
		return spec.AttemptRecord{}, false, nil
	}
	return attempt, true, nil
}

func (r *MemoryRegistry) AllocateCurrentAttempt(_ context.Context, runID, nodeID string) (spec.AttemptRecord, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	node, err := r.lockedGetNode(runID, nodeID)
	if err != nil {
		return spec.AttemptRecord{}, err
	}
	// Reject if the current Attempt exists and is non-terminal: no replacement
	// Attempt may be created while the current one is unresolved.
	if node.CurrentAttemptID != "" {
		if cur, ok := r.attempts[runID][nodeID][node.CurrentAttemptID]; ok && !cur.Status.IsTerminal() {
			return spec.AttemptRecord{}, ErrAttemptNonTerminal
		}
	}
	// F3-B3: this allocates a REALIZATION cycle (pre-user-code preparation), NOT a
	// user-code execution opportunity. It increments the separate
	// RealizationAttemptCount and derives the attempt id from it; the user-code
	// opportunity budget (AttemptCount vs MaxAttempts) is consumed only when the
	// semantic Attempt opens at the submission fence (OpenSemanticAttempt).
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
	// All-or-nothing: mutate the in-memory maps only after all checks pass.
	r.attempts[runID][nodeID][attemptID] = attempt
	node.RealizationAttemptCount = next
	node.CurrentAttemptID = attemptID
	node.Status = spec.NodeStatusReady
	node.CurrentBottleneckLocation = "release_wait"
	node.StartedAt = &now
	r.nodes[runID][nodeID] = node
	return attempt, nil
}

// OpenSemanticAttempt records that the current reservation's submission fence was
// crossed — the semantic Attempt opens here and consumes one user-code execution
// opportunity (AttemptCount++). Callers MUST verify an opportunity slot is available
// (AttemptCount < RetryPolicy.MaxAttempts) BEFORE calling; the registry only applies
// the atomic AttemptCount increment + fence timestamp on the current attempt.
func (r *MemoryRegistry) OpenSemanticAttempt(_ context.Context, runID, nodeID, attemptID string, openedAt time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	node, err := r.lockedGetNode(runID, nodeID)
	if err != nil {
		return err
	}
	if node.CurrentAttemptID != attemptID {
		return ErrAttemptNotFound
	}
	if err := r.lockedMutateAttempt(runID, nodeID, attemptID, func(a *spec.AttemptRecord) {
		t := openedAt.UTC()
		a.SubmissionWindowOpenedAt = &t
	}); err != nil {
		return err
	}
	node.AttemptCount++
	r.nodes[runID][nodeID] = node
	return nil
}

func (r *MemoryRegistry) PersistSubmissionFence(_ context.Context, runID, nodeID, attemptID string, openedAt time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.lockedMutateAttempt(runID, nodeID, attemptID, func(a *spec.AttemptRecord) {
		t := openedAt.UTC()
		a.SubmissionWindowOpenedAt = &t
	})
}

func (r *MemoryRegistry) PersistBackendHandle(_ context.Context, runID, nodeID, attemptID, handleJSON string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.lockedMutateAttempt(runID, nodeID, attemptID, func(a *spec.AttemptRecord) {
		a.BackendHandleJSON = handleJSON
	}); err != nil {
		return err
	}
	// Compat projection: mirror the current Attempt's handle onto the node.
	node := r.nodes[runID][nodeID]
	if node.CurrentAttemptID == attemptID {
		node.CurrentAttemptHandleJSON = handleJSON
		r.nodes[runID][nodeID] = node
	}
	return nil
}

func (r *MemoryRegistry) PersistCancellationIntent(_ context.Context, runID, nodeID, attemptID string, requestedAt time.Time, reason string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.lockedMutateAttempt(runID, nodeID, attemptID, func(a *spec.AttemptRecord) {
		t := requestedAt.UTC()
		a.CancellationRequestedAt = &t
		a.CancellationReason = reason
	})
}

func (r *MemoryRegistry) PersistProcessCompleted(_ context.Context, runID, nodeID, attemptID string, completedAt time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.lockedMutateAttempt(runID, nodeID, attemptID, func(a *spec.AttemptRecord) {
		t := completedAt.UTC()
		a.ProcessCompletedAt = &t
	})
}

func (r *MemoryRegistry) lockedGetNode(runID, nodeID string) (spec.NodeRecord, error) {
	runNodes, ok := r.nodes[runID]
	if !ok {
		return spec.NodeRecord{}, ErrRunNotFound
	}
	node, ok := runNodes[nodeID]
	if !ok {
		return spec.NodeRecord{}, ErrNodeNotFound
	}
	return node, nil
}

func (r *MemoryRegistry) lockedMutateAttempt(runID, nodeID, attemptID string, mutate func(*spec.AttemptRecord)) error {
	runAttempts, ok := r.attempts[runID]
	if !ok {
		return ErrRunNotFound
	}
	nodeAttempts, ok := runAttempts[nodeID]
	if !ok {
		return ErrNodeNotFound
	}
	attempt, ok := nodeAttempts[attemptID]
	if !ok {
		return ErrAttemptNotFound
	}
	mutate(&attempt)
	nodeAttempts[attemptID] = attempt
	return nil
}
