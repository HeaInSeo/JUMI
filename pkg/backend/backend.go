package backend

import (
	"context"

	"github.com/HeaInSeo/JUMI/pkg/spec"
)

type Backend interface {
	Prepare(ctx context.Context, run spec.RunRecord, node spec.NodeRecord) error
	Start(ctx context.Context, run spec.RunRecord, node spec.NodeRecord) error
	Wait(ctx context.Context, run spec.RunRecord, node spec.NodeRecord) error
	Cancel(ctx context.Context, run spec.RunRecord, node spec.NodeRecord) error
}

type OptionalKueueInfo struct {
	Observed      bool   `json:"observed"`
	QueueName     string `json:"queueName,omitempty"`
	WorkloadName  string `json:"workloadName,omitempty"`
	PendingReason string `json:"pendingReason,omitempty"`
}
