package executor

import (
	"context"
	"time"

	"github.com/HeaInSeo/JUMI/pkg/registry"
	"github.com/HeaInSeo/JUMI/pkg/spec"
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
	now := time.Now().UTC()
	if err := e.registry.UpdateRun(ctx, runID, func(run *spec.RunRecord) error {
		run.Status = spec.RunStatusCanceled
		run.TerminalStopCause = "canceled"
		run.FinishedAt = &now
		return nil
	}); err != nil {
		return err
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
