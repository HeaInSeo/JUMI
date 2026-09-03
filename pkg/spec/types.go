package spec

import (
	"strconv"
	"time"
)

type RunStatus string

const (
	RunStatusAccepted  RunStatus = "Accepted"
	RunStatusAdmitted  RunStatus = "Admitted"
	RunStatusRunning   RunStatus = "Running"
	RunStatusSucceeded RunStatus = "Succeeded"
	RunStatusFailed    RunStatus = "Failed"
	RunStatusCanceled  RunStatus = "Canceled"
)

type NodeStatus string

const (
	NodeStatusPending          NodeStatus = "Pending"
	NodeStatusReady            NodeStatus = "Ready"
	NodeStatusBuildingBindings NodeStatus = "BuildingBindings"
	NodeStatusResolvingInputs  NodeStatus = "ResolvingInputs"
	NodeStatusReleasing        NodeStatus = "Releasing"
	NodeStatusStarting         NodeStatus = "Starting"
	NodeStatusRunning          NodeStatus = "Running"
	NodeStatusSucceeded        NodeStatus = "Succeeded"
	NodeStatusFailed           NodeStatus = "Failed"
	NodeStatusCanceled         NodeStatus = "Canceled"
	NodeStatusSkipped          NodeStatus = "Skipped"
)

type AttemptStatus string

const (
	AttemptStatusPrepared  AttemptStatus = "Prepared"
	AttemptStatusStarted   AttemptStatus = "Started"
	AttemptStatusCompleted AttemptStatus = "Completed"
	AttemptStatusErrored   AttemptStatus = "Errored"
)

// IsTerminal reports whether an Attempt has reached authoritative terminal
// truth. Attempt terminal truth is the execution authority for F3 durable
// execution; Node/Run status and counters are projections of it.
func (s AttemptStatus) IsTerminal() bool {
	switch s {
	case AttemptStatusCompleted, AttemptStatusErrored:
		return true
	default:
		return false
	}
}

// IsTerminal reports whether a Node has reached a terminal projection state.
func (s NodeStatus) IsTerminal() bool {
	switch s {
	case NodeStatusSucceeded, NodeStatusFailed, NodeStatusCanceled, NodeStatusSkipped:
		return true
	default:
		return false
	}
}

// IsTerminal reports whether a Run has reached a terminal projection state.
func (s RunStatus) IsTerminal() bool {
	switch s {
	case RunStatusSucceeded, RunStatusFailed, RunStatusCanceled:
		return true
	default:
		return false
	}
}

// DeterministicAttemptID derives the canonical Attempt identity from
// (runID, nodeID, attempt-number). The identity is deterministic so that a
// crashed submission can be reconciled by re-deriving the same Attempt id
// rather than allocating a replacement Attempt (F3 reconcile-first).
func DeterministicAttemptID(runID, nodeID string, attempt int) string {
	return runID + "-" + nodeID + "-attempt-" + strconv.Itoa(attempt)
}

type ExecutableRunSpec struct {
	Run      RunMetadata       `json:"run"`
	Graph    Graph             `json:"graph"`
	Defaults Defaults          `json:"defaults,omitempty"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

type RunMetadata struct {
	RunID         string        `json:"runId"`
	SampleRunID   string        `json:"sampleRunId,omitempty"`
	SubmittedAt   time.Time     `json:"submittedAt"`
	FailurePolicy FailurePolicy `json:"failurePolicy"`
	RequesterID   string        `json:"requesterId,omitempty"`
	TraceID       string        `json:"traceId,omitempty"`
	SourceSystem  string        `json:"sourceSystem,omitempty"`
}

type FailurePolicy struct {
	Mode string `json:"mode"`
}

type Graph struct {
	Nodes []Node     `json:"nodes"`
	Edges [][]string `json:"edges"`
}

type Node struct {
	NodeID             string            `json:"nodeId"`
	Image              string            `json:"image"`
	Command            []string          `json:"command"`
	Args               []string          `json:"args"`
	Env                map[string]string `json:"env,omitempty"`
	ExecutionClass     string            `json:"executionClass,omitempty"`
	ResourceProfile    ResourceProfile   `json:"resourceProfile,omitempty"`
	TimeoutPolicy      TimeoutPolicy     `json:"timeoutPolicy,omitempty"`
	RetryPolicy        RetryPolicy       `json:"retryPolicy,omitempty"`
	Mounts             []Mount           `json:"mounts,omitempty"`
	Inputs             []string          `json:"inputs,omitempty"`
	Outputs            []string          `json:"outputs,omitempty"`
	ArtifactBindings   []ArtifactBinding `json:"artifactBindings,omitempty"`
	WorkingDir         string            `json:"workingDir,omitempty"`
	ServiceAccountName string            `json:"serviceAccountName,omitempty"`
	Placement          *PlacementHints   `json:"placement,omitempty"`
	CleanupPolicy      CleanupPolicy     `json:"cleanupPolicy,omitempty"`
	Metadata           map[string]string `json:"metadata,omitempty"`
	Kueue              *KueueHints       `json:"kueue,omitempty"`
}

type Defaults struct {
	ExecutionClass  string          `json:"executionClass,omitempty"`
	ResourceProfile ResourceProfile `json:"resourceProfile,omitempty"`
	TimeoutPolicy   TimeoutPolicy   `json:"timeoutPolicy,omitempty"`
	RetryPolicy     RetryPolicy     `json:"retryPolicy,omitempty"`
	CleanupPolicy   CleanupPolicy   `json:"cleanupPolicy,omitempty"`
	Placement       *PlacementHints `json:"placement,omitempty"`
	Namespace       string          `json:"namespace,omitempty"`
}

type ResourceProfile struct {
	CPU    string `json:"cpu,omitempty"`
	Memory string `json:"memory,omitempty"`
}

type TimeoutPolicy struct {
	Seconds int `json:"seconds,omitempty"`
}

type RetryPolicy struct {
	MaxAttempts     int      `json:"maxAttempts,omitempty"`
	RetryablePhases []string `json:"retryablePhases,omitempty"`
	RetryDelayHint  string   `json:"retryDelayHint,omitempty"`
}

type CleanupPolicy struct {
	TTLSecondsAfterFinished int32 `json:"ttlSecondsAfterFinished,omitempty"`
}

type WeightedNodePreference struct {
	NodeName string `json:"nodeName,omitempty"`
	Weight   int32  `json:"weight,omitempty"`
}

type PlacementHints struct {
	NodeSelector     map[string]string        `json:"nodeSelector,omitempty"`
	RequiredNodeName string                   `json:"requiredNodeName,omitempty"`
	PreferredNodes   []WeightedNodePreference `json:"preferredNodes,omitempty"`
}

type Mount struct {
	Source string `json:"source"`
	Target string `json:"target"`
	Mode   string `json:"mode,omitempty"`
}

type ArtifactBinding struct {
	BindingName        string `json:"bindingName"`
	ChildInputName     string `json:"childInputName,omitempty"`
	ProducerNodeID     string `json:"producerNodeId"`
	ProducerOutputName string `json:"producerOutputName"`
	ArtifactID         string `json:"artifactId,omitempty"`
	Required           bool   `json:"required,omitempty"`
	ConsumePolicy      string `json:"consumePolicy,omitempty"`
	ExpectedDigest     string `json:"expectedDigest,omitempty"`
}

type KueueHints struct {
	QueueName     string            `json:"queueName,omitempty"`
	WorkloadClass string            `json:"workloadClass,omitempty"`
	Labels        map[string]string `json:"labels,omitempty"`
}

type NodeObservation struct {
	KueueObserved       bool   `json:"kueueObserved,omitempty"`
	QueueName           string `json:"queueName,omitempty"`
	WorkloadName        string `json:"workloadName,omitempty"`
	KueuePendingReason  string `json:"kueuePendingReason,omitempty"`
	KueueAdmitted       bool   `json:"kueueAdmitted,omitempty"`
	PodName             string `json:"podName,omitempty"`
	PodNodeName         string `json:"podNodeName,omitempty"`
	PodScheduled        bool   `json:"podScheduled,omitempty"`
	UnschedulableReason string `json:"unschedulableReason,omitempty"`
}

type RunRecord struct {
	RunID                     string            `json:"runId"`
	Status                    RunStatus         `json:"status"`
	AcceptedAt                time.Time         `json:"acceptedAt"`
	StartedAt                 *time.Time        `json:"startedAt,omitempty"`
	FinishedAt                *time.Time        `json:"finishedAt,omitempty"`
	CurrentBottleneckLocation string            `json:"currentBottleneckLocation,omitempty"`
	TerminalStopCause         string            `json:"terminalStopCause,omitempty"`
	TerminalFailureReason     string            `json:"terminalFailureReason,omitempty"`
	Counters                  RunCounters       `json:"counters,omitempty"`
	Metadata                  map[string]string `json:"metadata,omitempty"`
	Spec                      ExecutableRunSpec `json:"spec"`
}

type RunCounters struct {
	TotalNodes     int `json:"totalNodes,omitempty"`
	SucceededNodes int `json:"succeededNodes,omitempty"`
	FailedNodes    int `json:"failedNodes,omitempty"`
	CanceledNodes  int `json:"canceledNodes,omitempty"`
	SkippedNodes   int `json:"skippedNodes,omitempty"`
	RunningNodes   int `json:"runningNodes,omitempty"`
}

type NodeRecord struct {
	RunID                     string     `json:"runId"`
	NodeID                    string     `json:"nodeId"`
	Status                    NodeStatus `json:"status"`
	CurrentBottleneckLocation string     `json:"currentBottleneckLocation,omitempty"`
	TerminalStopCause         string     `json:"terminalStopCause,omitempty"`
	TerminalFailureReason     string     `json:"terminalFailureReason,omitempty"`
	AttemptCount              int        `json:"attemptCount,omitempty"`
	// RealizationAttemptCount counts pre-user-code realization cycles (binding /
	// placement / prepare / fence work, and new-workload-after-authoritative-E0).
	// It is a SEPARATE finite budget from AttemptCount (the user-authored
	// execution-opportunity budget capped by RetryPolicy.MaxAttempts): a
	// realization-only failure before the submission fence must not consume a
	// user-code execution opportunity (F3-B3). Its ceiling is an internal safety
	// bound independent of MaxAttempts.
	RealizationAttemptCount  int             `json:"realizationAttemptCount,omitempty"`
	CurrentAttemptID         string          `json:"currentAttemptId,omitempty"`
	CurrentAttemptHandleJSON string          `json:"currentAttemptHandleJson,omitempty"`
	StartedAt                *time.Time      `json:"startedAt,omitempty"`
	FinishedAt               *time.Time      `json:"finishedAt,omitempty"`
	Observation              NodeObservation `json:"observation,omitempty"`
}

type AttemptRecord struct {
	RunID                 string        `json:"runId"`
	NodeID                string        `json:"nodeId"`
	AttemptID             string        `json:"attemptId"`
	Status                AttemptStatus `json:"status"`
	StartedAt             *time.Time    `json:"startedAt,omitempty"`
	FinishedAt            *time.Time    `json:"finishedAt,omitempty"`
	TerminalStopCause     string        `json:"terminalStopCause,omitempty"`
	TerminalFailureReason string        `json:"terminalFailureReason,omitempty"`

	// Durable F3 execution-truth facts (Closure Sprint B Packet A). These are
	// the authoritative, restart-surviving facts from which reconcile derives
	// its decision; do NOT introduce broad canonical enums for them.

	// BackendHandleJSON is the authoritative serialized backend handle for this
	// Attempt's execution. The node-level CurrentAttemptHandleJSON remains as a
	// compatibility projection of the current Attempt's handle.
	BackendHandleJSON string `json:"backendHandleJson,omitempty"`
	// SubmissionWindowOpenedAt records that the submission fence was crossed for
	// this Attempt BEFORE the backend side-effect boundary (StartNode). If set,
	// the backend submit outcome is unknown-but-possibly-effected and MUST be
	// reconciled by deterministic Attempt identity, never by blind replacement.
	SubmissionWindowOpenedAt *time.Time `json:"submissionWindowOpenedAt,omitempty"`
	// CancellationRequestedAt records an accepted cancellation intent as a
	// restart-surviving durable fact until terminal truth is confirmed.
	CancellationRequestedAt *time.Time `json:"cancellationRequestedAt,omitempty"`
	// CancellationReason is the reason carried with the accepted cancel intent.
	CancellationReason string `json:"cancellationReason,omitempty"`
	// ProcessCompletedAt records that the user process for this Attempt completed
	// successfully (Q32 E4) and only platform finalization (artifact registration /
	// terminal notification) remains. It is a durable, restart-surviving execution
	// outcome fact: once set, recovery must reconcile finalization on the SAME
	// execution and MUST NEVER re-run user code, even if the backend Job has since
	// been garbage-collected and can no longer be re-observed (F3-B2 / #46).
	ProcessCompletedAt *time.Time `json:"processCompletedAt,omitempty"`
}

type EventRecord struct {
	RunID         string    `json:"runId"`
	NodeID        string    `json:"nodeId,omitempty"`
	AttemptID     string    `json:"attemptId,omitempty"`
	Type          string    `json:"type"`
	Message       string    `json:"message,omitempty"`
	OccurredAt    time.Time `json:"occurredAt"`
	Level         string    `json:"level,omitempty"`
	StopCause     string    `json:"stopCause,omitempty"`
	FailureReason string    `json:"failureReason,omitempty"`
}
