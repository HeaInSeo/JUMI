package registry

import (
	"context"

	"github.com/HeaInSeo/JUMI/pkg/spec"
)

type Registry interface {
	CreateRun(ctx context.Context, record spec.RunRecord, nodes []spec.NodeRecord) error
	GetRun(ctx context.Context, runID string) (spec.RunRecord, error)
	ListNodes(ctx context.Context, runID string) ([]spec.NodeRecord, error)
	UpdateRun(ctx context.Context, runID string, update func(*spec.RunRecord) error) error
	UpdateNode(ctx context.Context, runID, nodeID string, update func(*spec.NodeRecord) error) error
}
