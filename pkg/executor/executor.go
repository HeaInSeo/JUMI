package executor

import (
	"context"
	"errors"
	"fmt"
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

type DagEngine struct {
	registry registry.Registry
	adapter  backend.Adapter
}

func NewDagEngine(reg registry.Registry, adapter backend.Adapter) *DagEngine {
	return &DagEngine{registry: reg, adapter: adapter}
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

func (e *DagEngine) Cancel(ctx context.Context, runID string, _ string) error {
	return markRunCanceled(ctx, e.registry, runID)
}

func (e *DagEngine) executeRun(runID string) {
	ctx := context.Background()
	run, err := e.registry.GetRun(ctx, runID)
	if err != nil {
		return
	}
	if err := e.runGraph(ctx, run); err != nil {
		_ = e.finalizeRun(ctx, runID, false, err.Error())
		return
	}
	_ = e.finalizeRun(ctx, runID, true, "")
}

func (e *DagEngine) runGraph(ctx context.Context, run spec.RunRecord) error {
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	d, err := dag.InitDag()
	if err != nil {
		return err
	}
	inDegree := make(map[string]int, len(run.Spec.Graph.Nodes))
	for _, node := range run.Spec.Graph.Nodes {
		inDegree[node.NodeID] = 0
		dagNode := d.CreateNode(node.NodeID)
		dagNode.RunCommand = &nodeRunner{registry: e.registry, adapter: e.adapter, runID: run.RunID, node: node}
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
	if !d.GetReady(runCtx) {
		return fmt.Errorf("failed to initialize dag worker pool")
	}
	_ = e.registry.UpdateRun(ctx, run.RunID, func(current *spec.RunRecord) error {
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
			case <-runCtx.Done():
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
						cancel()
						return
					}
				}
			}
		}
	}()
	if !d.Start() {
		return fmt.Errorf("failed to start dag")
	}
	ok := d.Wait(runCtx)
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
			_ = e.registry.UpdateNode(ctx, run.RunID, nodeID, func(current *spec.NodeRecord) error {
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
			status = spec.RunStatusCanceled
			terminalStopCause = "canceled"
		case spec.NodeStatusSkipped:
			counters.SkippedNodes++
		case spec.NodeStatusRunning, spec.NodeStatusStarting, spec.NodeStatusReleasing, spec.NodeStatusReady:
			counters.RunningNodes++
		}
	}
	if !succeeded && status == spec.RunStatusSucceeded {
		status = spec.RunStatusFailed
		terminalStopCause = "failed"
		if terminalFailureReason == "" {
			terminalFailureReason = reason
		}
	}
	return e.registry.UpdateRun(ctx, runID, func(run *spec.RunRecord) error {
		run.Status = status
		run.FinishedAt = &now
		run.Counters = counters
		run.TerminalStopCause = terminalStopCause
		run.TerminalFailureReason = terminalFailureReason
		run.CurrentBottleneckLocation = ""
		return nil
	})
}

type nodeRunner struct {
	registry registry.Registry
	adapter  backend.Adapter
	runID    string
	node     spec.Node
}

func (r *nodeRunner) RunE(_ interface{}) error {
	ctx := context.Background()
	if r.node.TimeoutPolicy.Seconds > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(r.node.TimeoutPolicy.Seconds)*time.Second)
		defer cancel()
	}
	run, err := r.registry.GetRun(ctx, r.runID)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	if err := r.registry.UpdateNode(ctx, r.runID, r.node.NodeID, func(current *spec.NodeRecord) error {
		current.Status = spec.NodeStatusStarting
		current.CurrentBottleneckLocation = "backend_start"
		current.StartedAt = &now
		return nil
	}); err != nil {
		return err
	}
	result, err := r.adapter.ExecuteNode(ctx, run, r.node)
	finishedAt := time.Now().UTC()
	if err != nil {
		_ = r.registry.UpdateNode(ctx, r.runID, r.node.NodeID, func(current *spec.NodeRecord) error {
			current.Status = spec.NodeStatusFailed
			current.TerminalStopCause = result.TerminalStopCause
			current.TerminalFailureReason = firstNonEmpty(result.TerminalFailureReason, "backend_wait_error")
			current.CurrentBottleneckLocation = ""
			current.FinishedAt = &finishedAt
			return nil
		})
		return err
	}
	return r.registry.UpdateNode(ctx, r.runID, r.node.NodeID, func(current *spec.NodeRecord) error {
		current.Status = spec.NodeStatusSucceeded
		current.TerminalStopCause = result.TerminalStopCause
		current.TerminalFailureReason = result.TerminalFailureReason
		current.CurrentBottleneckLocation = ""
		current.FinishedAt = &finishedAt
		return nil
	})
}

func markRunCanceled(ctx context.Context, reg registry.Registry, runID string) error {
	now := time.Now().UTC()
	if err := reg.UpdateRun(ctx, runID, func(run *spec.RunRecord) error {
		run.Status = spec.RunStatusCanceled
		run.TerminalStopCause = "canceled"
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
