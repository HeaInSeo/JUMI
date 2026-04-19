package registry

import (
	"context"
	"errors"
	"sort"
	"sync"

	"github.com/HeaInSeo/JUMI/pkg/spec"
)

var ErrRunNotFound = errors.New("run not found")
var ErrNodeNotFound = errors.New("node not found")
var ErrRunAlreadyExists = errors.New("run already exists")

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
