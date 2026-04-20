package backend

import (
	"context"

	"github.com/HeaInSeo/JUMI/pkg/spec"
)

type PreparedNode any

type Handle any

type ExecutionResult struct {
	Succeeded             bool               `json:"succeeded"`
	TerminalStopCause     string             `json:"terminalStopCause,omitempty"`
	TerminalFailureReason string             `json:"terminalFailureReason,omitempty"`
	Kueue                 *OptionalKueueInfo `json:"kueue,omitempty"`
}

type Adapter interface {
	PrepareNode(ctx context.Context, run spec.RunRecord, node spec.Node) (PreparedNode, error)
	StartNode(ctx context.Context, prepared PreparedNode) (Handle, error)
	ObserveNode(ctx context.Context, handle Handle) (*OptionalKueueInfo, error)
	WaitNode(ctx context.Context, handle Handle) (ExecutionResult, error)
	CancelNode(ctx context.Context, handle Handle) error
}

type OptionalKueueInfo struct {
	Observed            bool   `json:"observed"`
	QueueName           string `json:"queueName,omitempty"`
	WorkloadName        string `json:"workloadName,omitempty"`
	PendingReason       string `json:"pendingReason,omitempty"`
	Admitted            bool   `json:"admitted,omitempty"`
	PodName             string `json:"podName,omitempty"`
	Scheduled           bool   `json:"scheduled,omitempty"`
	UnschedulableReason string `json:"unschedulableReason,omitempty"`
}
