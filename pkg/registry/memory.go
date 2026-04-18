package registry

import (
	"context"
	"errors"
	"sync"

	"github.com/HeaInSeo/JUMI/pkg/spec"
)

var ErrRunNotFound = errors.New("run not found")
var ErrNodeNotFound = errors.New("node not found")
var ErrRunAlreadyExists = errors.New("run already exists")

type MemoryRegistry struct {
	mu    sync.RWMutex
	runs  map[string]spec.RunRecord
	nodes map[string]map[string]spec.NodeRecord
}

func NewMemoryRegistry() *MemoryRegistry {
	return &MemoryRegistry{
		runs:  make(map[string]spec.RunRecord),
		nodes: make(map[string]map[string]spec.NodeRecord),
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
	for _, node := range nodes {
		r.nodes[record.RunID][node.NodeID] = node
	}
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
