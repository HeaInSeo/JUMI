package backend

import (
	"context"

	"github.com/HeaInSeo/JUMI/pkg/spec"
)

type ExecutionResult struct {
	Succeeded             bool               `json:"succeeded"`
	TerminalStopCause     string             `json:"terminalStopCause,omitempty"`
	TerminalFailureReason string             `json:"terminalFailureReason,omitempty"`
	Kueue                 *OptionalKueueInfo `json:"kueue,omitempty"`
}

type Adapter interface {
	ExecuteNode(ctx context.Context, run spec.RunRecord, node spec.Node) (ExecutionResult, error)
}

type OptionalKueueInfo struct {
	Observed      bool   `json:"observed"`
	QueueName     string `json:"queueName,omitempty"`
	WorkloadName  string `json:"workloadName,omitempty"`
	PendingReason string `json:"pendingReason,omitempty"`
}
