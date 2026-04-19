package spec

import "time"

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
	NodeStatusPending   NodeStatus = "Pending"
	NodeStatusReady     NodeStatus = "Ready"
	NodeStatusReleasing NodeStatus = "Releasing"
	NodeStatusStarting  NodeStatus = "Starting"
	NodeStatusRunning   NodeStatus = "Running"
	NodeStatusSucceeded NodeStatus = "Succeeded"
	NodeStatusFailed    NodeStatus = "Failed"
	NodeStatusCanceled  NodeStatus = "Canceled"
	NodeStatusSkipped   NodeStatus = "Skipped"
)

type AttemptStatus string

const (
	AttemptStatusPrepared  AttemptStatus = "Prepared"
	AttemptStatusStarted   AttemptStatus = "Started"
	AttemptStatusCompleted AttemptStatus = "Completed"
	AttemptStatusErrored   AttemptStatus = "Errored"
)

type ExecutableRunSpec struct {
	Run      RunMetadata       `json:"run"`
	Graph    Graph             `json:"graph"`
	Defaults Defaults          `json:"defaults,omitempty"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

type RunMetadata struct {
	RunID         string        `json:"runId"`
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
	WorkingDir         string            `json:"workingDir,omitempty"`
	ServiceAccountName string            `json:"serviceAccountName,omitempty"`
	Metadata           map[string]string `json:"metadata,omitempty"`
	Kueue              *KueueHints       `json:"kueue,omitempty"`
}

type Defaults struct {
	ExecutionClass  string          `json:"executionClass,omitempty"`
	ResourceProfile ResourceProfile `json:"resourceProfile,omitempty"`
	TimeoutPolicy   TimeoutPolicy   `json:"timeoutPolicy,omitempty"`
	RetryPolicy     RetryPolicy     `json:"retryPolicy,omitempty"`
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

type Mount struct {
	Source string `json:"source"`
	Target string `json:"target"`
	Mode   string `json:"mode,omitempty"`
}

type KueueHints struct {
	QueueName     string            `json:"queueName,omitempty"`
	WorkloadClass string            `json:"workloadClass,omitempty"`
	Labels        map[string]string `json:"labels,omitempty"`
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
	CurrentAttemptID          string     `json:"currentAttemptId,omitempty"`
	StartedAt                 *time.Time `json:"startedAt,omitempty"`
	FinishedAt                *time.Time `json:"finishedAt,omitempty"`
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
