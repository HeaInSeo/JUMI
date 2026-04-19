package executor

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/HeaInSeo/JUMI/pkg/backend"
	"github.com/HeaInSeo/JUMI/pkg/registry"
	"github.com/HeaInSeo/JUMI/pkg/spec"
	dag "github.com/seoyhaein/dag-go"
)

type Engine interface {
	Admit(ctx context.Context, record spec.RunRecord) error
	Cancel(ctx context.Context, runID string, reason string) error
}

type NoopEngine struct {
	registry registry.Registry
}

func NewNoopEngine(reg registry.Registry) *NoopEngine {
	return &NoopEngine{registry: reg}
}

func (e *NoopEngine) Admit(ctx context.Context, record spec.RunRecord) error {
	now := time.Now().UTC()
	return e.registry.UpdateRun(ctx, record.RunID, func(run *spec.RunRecord) error {
		run.Status = spec.RunStatusAdmitted
		run.CurrentBottleneckLocation = "dispatch_wait"
		run.StartedAt = &now
		return nil
	})
}

func (e *NoopEngine) Cancel(ctx context.Context, runID string, _ string) error {
	return markRunCanceled(ctx, e.registry, runID)
}

type activeRun struct {
	cancel  context.CancelFunc
	mu      sync.Mutex
	handles map[string]backend.Handle
}

type DagEngine struct {
	registry registry.Registry
	adapter  backend.Adapter

	activeMu sync.Mutex
	active   map[string]*activeRun
}

func NewDagEngine(reg registry.Registry, adapter backend.Adapter) *DagEngine {
	return &DagEngine{registry: reg, adapter: adapter, active: make(map[string]*activeRun)}
}

func (e *DagEngine) Admit(ctx context.Context, record spec.RunRecord) error {
	now := time.Now().UTC()
	if err := e.registry.UpdateRun(ctx, record.RunID, func(run *spec.RunRecord) error {
		run.Status = spec.RunStatusAdmitted
		run.CurrentBottleneckLocation = "dispatch_wait"
		run.StartedAt = &now
		return nil
	}); err != nil {
		return err
	}
	go e.executeRun(record.RunID)
	return nil
}

func (e *DagEngine) Cancel(ctx context.Context, runID string, reason string) error {
	now := time.Now().UTC()
	if err := e.registry.UpdateRun(ctx, runID, func(run *spec.RunRecord) error {
		switch run.Status {
		case spec.RunStatusSucceeded, spec.RunStatusFailed, spec.RunStatusCanceled:
			return nil
		default:
			run.Status = spec.RunStatusCanceled
			run.TerminalStopCause = "canceled"
			run.TerminalFailureReason = firstNonEmpty(reason, "cancellation_requested")
			run.CurrentBottleneckLocation = ""
			return nil
		}
	}); err != nil {
		return err
	}
	active := e.getActiveRun(runID)
	if active != nil {
		active.cancel()
		for _, handle := range e.snapshotHandles(active) {
			_ = e.adapter.CancelNode(context.Background(), handle)
		}
	}
	nodes, err := e.registry.ListNodes(ctx, runID)
	if err != nil {
		return err
	}
	for _, node := range nodes {
		nodeID := node.NodeID
		_ = e.registry.UpdateNode(ctx, runID, nodeID, func(current *spec.NodeRecord) error {
			switch current.Status {
			case spec.NodeStatusSucceeded, spec.NodeStatusFailed, spec.NodeStatusCanceled, spec.NodeStatusSkipped:
				return nil
			case spec.NodeStatusPending, spec.NodeStatusReady, spec.NodeStatusReleasing:
				current.Status = spec.NodeStatusCanceled
				current.TerminalStopCause = "canceled"
				current.TerminalFailureReason = firstNonEmpty(reason, "cancellation_requested")
				current.CurrentBottleneckLocation = ""
				current.FinishedAt = &now
			default:
				current.CurrentBottleneckLocation = "canceling"
			}
			return nil
		})
	}
	return nil
}

func (e *DagEngine) executeRun(runID string) {
	ctx := context.Background()
	run, err := e.registry.GetRun(ctx, runID)
	if err != nil {
		return
	}
	runCtx, cancel := context.WithCancel(ctx)
	active := &activeRun{cancel: cancel, handles: make(map[string]backend.Handle)}
	e.registerActiveRun(runID, active)
	defer func() {
		cancel()
		e.unregisterActiveRun(runID)
	}()
	if err := e.runGraph(runCtx, run, active); err != nil {
		_ = e.finalizeRun(ctx, runID, false, err.Error())
		return
	}
	_ = e.finalizeRun(ctx, runID, true, "")
}

func (e *DagEngine) runGraph(ctx context.Context, run spec.RunRecord, active *activeRun) error {
	d, err := dag.InitDag()
	if err != nil {
		return err
	}
	inDegree := make(map[string]int, len(run.Spec.Graph.Nodes))
	for _, node := range run.Spec.Graph.Nodes {
		inDegree[node.NodeID] = 0
		dagNode := d.CreateNode(node.NodeID)
		dagNode.RunCommand = &nodeRunner{registry: e.registry, adapter: e.adapter, runID: run.RunID, node: node, runCtx: ctx, active: active}
	}
	for _, edge := range run.Spec.Graph.Edges {
		if len(edge) != 2 {
			return fmt.Errorf("invalid edge shape")
		}
		if err := d.AddEdge(edge[0], edge[1]); err != nil {
			return err
		}
		inDegree[edge[1]]++
	}
	for nodeID, count := range inDegree {
		if count == 0 {
			if err := d.AddEdge(dag.StartNode, nodeID); err != nil {
				return err
			}
		}
	}
	if err := d.FinishDag(); err != nil {
		return err
	}
	if !d.ConnectRunner() {
		return fmt.Errorf("failed to connect runners")
	}
	if !d.GetReady(ctx) {
		return fmt.Errorf("failed to initialize dag worker pool")
	}
	_ = e.registry.UpdateRun(context.Background(), run.RunID, func(current *spec.RunRecord) error {
		current.Status = spec.RunStatusRunning
		current.CurrentBottleneckLocation = "running"
		return nil
	})
	firstErr := make(chan error, 1)
	go func() {
		ticker := time.NewTicker(10 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				nodes, err := e.registry.ListNodes(context.Background(), run.RunID)
				if err != nil {
					continue
				}
				for _, node := range nodes {
					if node.Status == spec.NodeStatusFailed {
						select {
						case firstErr <- fmt.Errorf("node %s failed", node.NodeID):
						default:
						}
						active.cancel()
						return
					}
				}
			}
		}
	}()
	if !d.Start() {
		return fmt.Errorf("failed to start dag")
	}
	ok := d.Wait(ctx)
	if !ok {
		runErr := "dag execution failed"
		select {
		case err := <-firstErr:
			if err != nil {
				runErr = err.Error()
			}
		default:
		}
		for _, node := range run.Spec.Graph.Nodes {
			nodeID := node.NodeID
			_ = e.registry.UpdateNode(context.Background(), run.RunID, nodeID, func(current *spec.NodeRecord) error {
				if current.Status == spec.NodeStatusPending {
					current.Status = spec.NodeStatusSkipped
					current.TerminalFailureReason = "dependency_failed"
					current.TerminalStopCause = "failed"
				}
				return nil
			})
		}
		return errors.New(runErr)
	}
	return nil
}

func (e *DagEngine) finalizeRun(ctx context.Context, runID string, succeeded bool, reason string) error {
	run, err := e.registry.GetRun(ctx, runID)
	if err != nil {
		return err
	}
	nodes, err := e.registry.ListNodes(ctx, runID)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	counters := spec.RunCounters{TotalNodes: len(nodes)}
	status := spec.RunStatusSucceeded
	terminalStopCause := "finished"
	terminalFailureReason := ""
	for _, node := range nodes {
		switch node.Status {
		case spec.NodeStatusSucceeded:
			counters.SucceededNodes++
		case spec.NodeStatusFailed:
			counters.FailedNodes++
			status = spec.RunStatusFailed
			terminalStopCause = "failed"
			if terminalFailureReason == "" {
				terminalFailureReason = node.TerminalFailureReason
			}
		case spec.NodeStatusCanceled:
			counters.CanceledNodes++
			if status != spec.RunStatusFailed {
				status = spec.RunStatusCanceled
				terminalStopCause = "canceled"
				terminalFailureReason = firstNonEmpty(node.TerminalFailureReason, terminalFailureReason)
			}
		case spec.NodeStatusSkipped:
			counters.SkippedNodes++
		case spec.NodeStatusRunning, spec.NodeStatusStarting, spec.NodeStatusReleasing, spec.NodeStatusReady:
			counters.RunningNodes++
		}
	}
	if run.Status == spec.RunStatusCanceled {
		status = spec.RunStatusCanceled
		terminalStopCause = "canceled"
		terminalFailureReason = firstNonEmpty(run.TerminalFailureReason, terminalFailureReason, reason, "cancellation_requested")
	}
	if !succeeded && status == spec.RunStatusSucceeded {
		status = spec.RunStatusFailed
		terminalStopCause = "failed"
		if terminalFailureReason == "" {
			terminalFailureReason = reason
		}
	}
	return e.registry.UpdateRun(ctx, runID, func(current *spec.RunRecord) error {
		current.Status = status
		current.FinishedAt = &now
		current.Counters = counters
		current.TerminalStopCause = terminalStopCause
		current.TerminalFailureReason = terminalFailureReason
		current.CurrentBottleneckLocation = ""
		return nil
	})
}

type nodeRunner struct {
	registry registry.Registry
	adapter  backend.Adapter
	runID    string
	node     spec.Node
	runCtx   context.Context
	active   *activeRun
}

func (r *nodeRunner) RunE(_ interface{}) error {
	ctx := r.runCtx
	if r.node.TimeoutPolicy.Seconds > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(r.node.TimeoutPolicy.Seconds)*time.Second)
		defer cancel()
	}
	run, err := r.registry.GetRun(context.Background(), r.runID)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	if err := r.registry.UpdateNode(context.Background(), r.runID, r.node.NodeID, func(current *spec.NodeRecord) error {
		current.AttemptCount++
		current.CurrentAttemptID = fmt.Sprintf("%s-%s-attempt-%d", r.runID, r.node.NodeID, current.AttemptCount)
		current.Status = spec.NodeStatusReady
		current.CurrentBottleneckLocation = "release_wait"
		current.StartedAt = &now
		return nil
	}); err != nil {
		return err
	}
	if err := r.registry.UpdateNode(context.Background(), r.runID, r.node.NodeID, func(current *spec.NodeRecord) error {
		current.Status = spec.NodeStatusStarting
		current.CurrentBottleneckLocation = "backend_prepare"
		return nil
	}); err != nil {
		return err
	}
	prepared, err := r.adapter.PrepareNode(ctx, run, r.node)
	if err != nil {
		return r.failNode(err, "failed", "backend_prepare_error")
	}
	if err := r.registry.UpdateNode(context.Background(), r.runID, r.node.NodeID, func(current *spec.NodeRecord) error {
		current.Status = spec.NodeStatusReleasing
		current.CurrentBottleneckLocation = "release_wait"
		return nil
	}); err != nil {
		return err
	}
	handle, err := r.adapter.StartNode(ctx, prepared)
	if err != nil {
		return r.failNode(err, "failed", "backend_start_error")
	}
	r.registerHandle(handle)
	defer r.unregisterHandle()
	if err := r.registry.UpdateNode(context.Background(), r.runID, r.node.NodeID, func(current *spec.NodeRecord) error {
		current.Status = spec.NodeStatusRunning
		current.CurrentBottleneckLocation = "running"
		return nil
	}); err != nil {
		return err
	}
	result, err := r.adapter.WaitNode(ctx, handle)
	if err != nil {
		if r.isRunCanceled() || result.TerminalStopCause == "canceled" {
			return r.cancelNode(firstNonEmpty(result.TerminalFailureReason, "cancellation_requested"))
		}
		return r.failNode(err, firstNonEmpty(result.TerminalStopCause, "failed"), firstNonEmpty(result.TerminalFailureReason, "backend_wait_error"))
	}
	finishedAt := time.Now().UTC()
	return r.registry.UpdateNode(context.Background(), r.runID, r.node.NodeID, func(current *spec.NodeRecord) error {
		current.Status = spec.NodeStatusSucceeded
		current.TerminalStopCause = firstNonEmpty(result.TerminalStopCause, "finished")
		current.TerminalFailureReason = result.TerminalFailureReason
		current.CurrentBottleneckLocation = ""
		current.FinishedAt = &finishedAt
		return nil
	})
}

func (r *nodeRunner) failNode(cause error, terminalStopCause string, failureReason string) error {
	if r.isRunCanceled() {
		return r.cancelNode("cancellation_requested")
	}
	finishedAt := time.Now().UTC()
	_ = r.registry.UpdateNode(context.Background(), r.runID, r.node.NodeID, func(current *spec.NodeRecord) error {
		current.Status = spec.NodeStatusFailed
		current.TerminalStopCause = terminalStopCause
		current.TerminalFailureReason = failureReason
		current.CurrentBottleneckLocation = ""
		current.FinishedAt = &finishedAt
		return nil
	})
	return cause
}

func (r *nodeRunner) cancelNode(reason string) error {
	finishedAt := time.Now().UTC()
	return r.registry.UpdateNode(context.Background(), r.runID, r.node.NodeID, func(current *spec.NodeRecord) error {
		current.Status = spec.NodeStatusCanceled
		current.TerminalStopCause = "canceled"
		current.TerminalFailureReason = firstNonEmpty(reason, "cancellation_requested")
		current.CurrentBottleneckLocation = ""
		current.FinishedAt = &finishedAt
		return nil
	})
}

func (r *nodeRunner) isRunCanceled() bool {
	run, err := r.registry.GetRun(context.Background(), r.runID)
	if err != nil {
		return false
	}
	return run.Status == spec.RunStatusCanceled
}

func (r *nodeRunner) registerHandle(handle backend.Handle) {
	r.active.mu.Lock()
	defer r.active.mu.Unlock()
	r.active.handles[r.node.NodeID] = handle
}

func (r *nodeRunner) unregisterHandle() {
	r.active.mu.Lock()
	defer r.active.mu.Unlock()
	delete(r.active.handles, r.node.NodeID)
}

func (e *DagEngine) registerActiveRun(runID string, active *activeRun) {
	e.activeMu.Lock()
	defer e.activeMu.Unlock()
	e.active[runID] = active
}

func (e *DagEngine) unregisterActiveRun(runID string) {
	e.activeMu.Lock()
	defer e.activeMu.Unlock()
	delete(e.active, runID)
}

func (e *DagEngine) getActiveRun(runID string) *activeRun {
	e.activeMu.Lock()
	defer e.activeMu.Unlock()
	return e.active[runID]
}

func (e *DagEngine) snapshotHandles(active *activeRun) []backend.Handle {
	active.mu.Lock()
	defer active.mu.Unlock()
	handles := make([]backend.Handle, 0, len(active.handles))
	for _, handle := range active.handles {
		handles = append(handles, handle)
	}
	return handles
}

func markRunCanceled(ctx context.Context, reg registry.Registry, runID string) error {
	now := time.Now().UTC()
	if err := reg.UpdateRun(ctx, runID, func(run *spec.RunRecord) error {
		run.Status = spec.RunStatusCanceled
		run.TerminalStopCause = "canceled"
		run.TerminalFailureReason = "cancellation_requested"
		run.FinishedAt = &now
		return nil
	}); err != nil {
		return err
	}
	nodes, err := reg.ListNodes(ctx, runID)
	if err != nil {
		return err
	}
	for _, node := range nodes {
		nodeID := node.NodeID
		_ = reg.UpdateNode(ctx, runID, nodeID, func(current *spec.NodeRecord) error {
			switch current.Status {
			case spec.NodeStatusSucceeded, spec.NodeStatusFailed, spec.NodeStatusCanceled, spec.NodeStatusSkipped:
				return nil
			default:
				current.Status = spec.NodeStatusCanceled
				current.TerminalStopCause = "canceled"
				current.TerminalFailureReason = "cancellation_requested"
				current.FinishedAt = &now
				return nil
			}
		})
	}
	return nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
