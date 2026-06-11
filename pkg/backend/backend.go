package backend

import (
	"context"
	"errors"

	"github.com/HeaInSeo/JUMI/pkg/provenance"
	"github.com/HeaInSeo/JUMI/pkg/spec"
)

type PreparedNode any

type Handle any

var ErrOutputMetadataUnavailable = errors.New("output metadata unavailable")

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

type OutputMetadata struct {
	OutputName        string                        `json:"outputName"`
	URI               string                        `json:"uri,omitempty"`
	LogicalURI        string                        `json:"logicalUri,omitempty"`
	Digest            string                        `json:"digest,omitempty"`
	SizeBytes         int64                         `json:"sizeBytes,omitempty"`
	ProducerAttemptID string                        `json:"producerAttemptId,omitempty"`
	Locations         []provenance.ArtifactLocation `json:"locations,omitempty"`
	NodeName          string                        `json:"nodeName,omitempty"`
	PodName           string                        `json:"podName,omitempty"`
}

type OutputMetadataProvider interface {
	CollectOutputMetadata(ctx context.Context, handle Handle, node spec.Node) (map[string]OutputMetadata, error)
}

// HandlePersister is an optional interface that an Adapter may implement
// to enable AttemptHandle persistence across executor restarts.
// MarshalHandle returns (nil, nil) for handle types that do not support persistence.
type HandlePersister interface {
	MarshalHandle(h Handle) ([]byte, error)
	UnmarshalHandle(data []byte) (Handle, error)
}

type AdapterStatus struct {
	Ready                 bool `json:"ready"`
	ReleaseBounded        bool `json:"releaseBounded"`
	ReleaseInflight       int  `json:"releaseInflight,omitempty"`
	ReleaseSlotsAvailable int  `json:"releaseSlotsAvailable,omitempty"`
	ReleaseMaxConcurrent  int  `json:"releaseMaxConcurrent,omitempty"`
}

type StatusProvider interface {
	AdapterStatus() AdapterStatus
}

type OptionalKueueInfo struct {
	Observed            bool   `json:"observed"`
	QueueName           string `json:"queueName,omitempty"`
	WorkloadName        string `json:"workloadName,omitempty"`
	PendingReason       string `json:"pendingReason,omitempty"`
	Admitted            bool   `json:"admitted,omitempty"`
	PodName             string `json:"podName,omitempty"`
	PodNodeName         string `json:"podNodeName,omitempty"`
	Scheduled           bool   `json:"scheduled,omitempty"`
	UnschedulableReason string `json:"unschedulableReason,omitempty"`
}
