package executor

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/HeaInSeo/JUMI/pkg/backend"
	"github.com/HeaInSeo/JUMI/pkg/handoff"
	"github.com/HeaInSeo/JUMI/pkg/metrics"
	"github.com/HeaInSeo/JUMI/pkg/provenance"
	"github.com/HeaInSeo/JUMI/pkg/registry"
	"github.com/HeaInSeo/JUMI/pkg/spec"
	dag "github.com/HeaInSeo/dag-go"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
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
	if err := e.registry.UpdateRun(ctx, record.RunID, func(run *spec.RunRecord) error {
		run.Status = spec.RunStatusAdmitted
		run.CurrentBottleneckLocation = "dispatch_wait"
		run.StartedAt = &now
		return nil
	}); err != nil {
		return err
	}
	appendEvent(ctx, e.registry, spec.EventRecord{RunID: record.RunID, Type: "run.admitted", OccurredAt: now, Level: "info", Message: "run admitted to executor"})
	return nil
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
	handoff  handoff.Client
	metrics  *metrics.Metrics

	activeMu sync.Mutex
	active   map[string]*activeRun
}

func newMetrics() *metrics.Metrics {
	m, err := metrics.New()
	if err != nil {
		panic(fmt.Sprintf("jumi: metrics init failed: %v", err))
	}
	return m
}

func NewDagEngine(reg registry.Registry, adapter backend.Adapter) *DagEngine {
	return &DagEngine{
		registry: reg,
		adapter:  adapter,
		handoff:  handoff.NewNoopClient(),
		metrics:  newMetrics(),
		active:   make(map[string]*activeRun),
	}
}

func NewDagEngineWithHandoff(reg registry.Registry, adapter backend.Adapter, client handoff.Client) *DagEngine {
	if client == nil {
		client = handoff.NewNoopClient()
	}
	return &DagEngine{
		registry: reg,
		adapter:  adapter,
		handoff:  client,
		metrics:  newMetrics(),
		active:   make(map[string]*activeRun),
	}
}

func (e *DagEngine) Metrics() *metrics.Metrics {
	return e.metrics
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
	appendEvent(ctx, e.registry, spec.EventRecord{RunID: record.RunID, Type: "run.admitted", OccurredAt: now, Level: "info", Message: "run admitted to dag executor"})
	runCtx := context.WithoutCancel(ctx)
	// #nosec G118 -- run execution must outlive the request context once the run is admitted.
	go e.executeRun(runCtx, record.RunID)
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
	appendEvent(ctx, e.registry, spec.EventRecord{RunID: runID, Type: "run.cancel.requested", OccurredAt: now, Level: "warn", StopCause: "canceled", FailureReason: firstNonEmpty(reason, "cancellation_requested")})
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
		canceledImmediately := false
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
				canceledImmediately = true
			default:
				current.CurrentBottleneckLocation = "canceling"
			}
			return nil
		})
		if canceledImmediately {
			appendEvent(ctx, e.registry, spec.EventRecord{RunID: runID, NodeID: nodeID, Type: "node.canceled", OccurredAt: now, Level: "warn", StopCause: "canceled", FailureReason: firstNonEmpty(reason, "cancellation_requested")})
		}
	}
	return nil
}

func (e *DagEngine) executeRun(ctx context.Context, runID string) {
	if ctx == nil {
		ctx = context.Background()
	}
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
	runners := make(map[string]dag.Runnable, len(run.Spec.Graph.Nodes))
	for _, node := range run.Spec.Graph.Nodes {
		inDegree[node.NodeID] = 0
		_ = d.CreateNode(node.NodeID)
		runners[node.NodeID] = &nodeRunner{registry: e.registry, adapter: e.adapter, handoff: e.handoff, metrics: e.metrics, runID: run.RunID, active: active, node: node}
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
	if _, missing, _ := d.SetNodeRunners(runners); len(missing) > 0 {
		return fmt.Errorf("failed to set dag-go runners for nodes: %v", missing)
	}
	if !d.ConnectRunner() {
		return fmt.Errorf("failed to connect runners")
	}
	if !d.GetReady(ctx) {
		return fmt.Errorf("failed to initialize dag worker pool")
	}
	now := time.Now().UTC()
	_ = e.registry.UpdateRun(ctx, run.RunID, func(current *spec.RunRecord) error {
		current.Status = spec.RunStatusRunning
		current.CurrentBottleneckLocation = "running"
		return nil
	})
	appendEvent(ctx, e.registry, spec.EventRecord{RunID: run.RunID, Type: "run.running", OccurredAt: now, Level: "info", Message: "run execution started"})
	firstErr := make(chan error, 1)
	go func() {
		ticker := time.NewTicker(10 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				nodes, err := e.registry.ListNodes(ctx, run.RunID)
				if err != nil {
					continue
				}
				for _, node := range nodes {
					if node.Status == spec.NodeStatusFailed {
						select {
						case firstErr <- fmt.Errorf("node %s failed", node.NodeID):
						default:
						}
						e.metrics.IncFastFailTrigger()
						appendEvent(ctx, e.registry, spec.EventRecord{RunID: run.RunID, NodeID: node.NodeID, Type: "run.fast_fail.triggered", OccurredAt: time.Now().UTC(), Level: "warn", StopCause: "failed", FailureReason: firstNonEmpty(node.TerminalFailureReason, "fast_fail")})
						active.cancel()
						for _, handle := range e.snapshotHandles(active) {
							_ = e.adapter.CancelNode(ctx, handle)
						}
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
			skipped := false
			occurredAt := time.Now().UTC()
			_ = e.registry.UpdateNode(context.Background(), run.RunID, nodeID, func(current *spec.NodeRecord) error {
				if current.Status == spec.NodeStatusPending {
					current.Status = spec.NodeStatusSkipped
					current.TerminalFailureReason = "dependency_failed"
					current.TerminalStopCause = "failed"
					skipped = true
				}
				return nil
			})
			if skipped {
				appendEvent(context.Background(), e.registry, spec.EventRecord{RunID: run.RunID, NodeID: nodeID, Type: "node.skipped", OccurredAt: occurredAt, Level: "warn", StopCause: "failed", FailureReason: "dependency_failed"})
			}
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
	runStatus := spec.RunStatusSucceeded
	terminalStopCause := "finished"
	terminalFailureReason := ""
	for _, node := range nodes {
		switch node.Status {
		case spec.NodeStatusSucceeded:
			counters.SucceededNodes++
		case spec.NodeStatusFailed:
			counters.FailedNodes++
			runStatus = spec.RunStatusFailed
			terminalStopCause = "failed"
			if terminalFailureReason == "" {
				terminalFailureReason = node.TerminalFailureReason
			}
		case spec.NodeStatusCanceled:
			counters.CanceledNodes++
			if runStatus != spec.RunStatusFailed {
				runStatus = spec.RunStatusCanceled
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
		runStatus = spec.RunStatusCanceled
		terminalStopCause = "canceled"
		terminalFailureReason = firstNonEmpty(run.TerminalFailureReason, terminalFailureReason, reason, "cancellation_requested")
	}
	if !succeeded && runStatus == spec.RunStatusSucceeded {
		runStatus = spec.RunStatusFailed
		terminalStopCause = "failed"
		if terminalFailureReason == "" {
			terminalFailureReason = reason
		}
	}
	if err := e.registry.UpdateRun(ctx, runID, func(current *spec.RunRecord) error {
		current.Status = runStatus
		current.FinishedAt = &now
		current.Counters = counters
		current.TerminalStopCause = terminalStopCause
		current.TerminalFailureReason = terminalFailureReason
		current.CurrentBottleneckLocation = ""
		return nil
	}); err != nil {
		return err
	}
	if err := e.handoff.FinalizeSampleRun(ctx, handoff.FinalizeSampleRunRequest{
		SampleRunID: firstNonEmpty(run.Spec.Run.SampleRunID, runID),
	}); err != nil {
		appendEvent(ctx, e.registry, spec.EventRecord{
			RunID:         runID,
			Type:          "run.handoff.finalize_failed",
			OccurredAt:    time.Now().UTC(),
			Level:         "error",
			StopCause:     "failed",
			FailureReason: "handoff_finalize_error",
			Message:       err.Error(),
		})
		if runStatus == spec.RunStatusSucceeded {
			runStatus = spec.RunStatusFailed
			terminalStopCause = "failed"
			terminalFailureReason = "handoff_finalize_error"
			if err := e.registry.UpdateRun(ctx, runID, func(current *spec.RunRecord) error {
				current.Status = runStatus
				current.TerminalStopCause = terminalStopCause
				current.TerminalFailureReason = terminalFailureReason
				return nil
			}); err != nil {
				return err
			}
		}
	} else {
		e.metrics.IncSampleRunsFinalized()
	}
	if err := e.handoff.EvaluateGC(ctx, handoff.EvaluateGCRequest{
		SampleRunID: firstNonEmpty(run.Spec.Run.SampleRunID, runID),
	}); err != nil {
		appendEvent(ctx, e.registry, spec.EventRecord{
			RunID:         runID,
			Type:          "run.handoff.gc_evaluate_failed",
			OccurredAt:    time.Now().UTC(),
			Level:         "error",
			StopCause:     "failed",
			FailureReason: "handoff_gc_evaluate_error",
			Message:       err.Error(),
		})
		if runStatus == spec.RunStatusSucceeded {
			runStatus = spec.RunStatusFailed
			terminalStopCause = "failed"
			terminalFailureReason = "handoff_gc_evaluate_error"
			if err := e.registry.UpdateRun(ctx, runID, func(current *spec.RunRecord) error {
				current.Status = runStatus
				current.TerminalStopCause = terminalStopCause
				current.TerminalFailureReason = terminalFailureReason
				return nil
			}); err != nil {
				return err
			}
		}
	} else {
		e.metrics.IncGCEvaluateRequests()
	}
	appendEvent(ctx, e.registry, spec.EventRecord{RunID: runID, Type: "run.completed", OccurredAt: now, Level: eventLevelForRunStatus(runStatus), StopCause: terminalStopCause, FailureReason: terminalFailureReason, Message: string(runStatus)})
	return nil
}

type nodeRunner struct {
	registry registry.Registry
	adapter  backend.Adapter
	handoff  handoff.Client
	metrics  *metrics.Metrics
	runID    string
	node     spec.Node
	active   *activeRun

	localityHints           []bindingLocalityHint
	localityFallbackStarted bool
	localityActualNode      string
}

type bindingLocalityHint struct {
	BindingName             string
	ChildInputName          string
	PreferredNodeName       string
	PlacementMode           string
	Decision                string
	RequiresMaterialization bool
}

type resolveAction int

const (
	resolveProceed resolveAction = iota
	resolveRetry
	resolveFail
	resolveSkipOptional
)

const (
	resolveBindingMaxAttempts          = 3
	resolveBindingRetryDelay           = 100 * time.Millisecond
	resolveBindingPendingMaxAttempts   = 5
	resolveBindingPendingInitialDelay  = 1 * time.Second
	resolveBindingPendingMaxDelay      = 10 * time.Second
	resolveBindingPendingBackoffFactor = 2
)

const hostnameNodeSelectorKey = "kubernetes.io/hostname"

func (r *nodeRunner) RunE(ctx context.Context, _ interface{}) error {
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
	attemptID := ""
	executionNode := cloneNode(r.node)
	if err := r.registry.UpdateNode(context.Background(), r.runID, r.node.NodeID, func(current *spec.NodeRecord) error {
		current.AttemptCount++
		attemptID = fmt.Sprintf("%s-%s-attempt-%d", r.runID, r.node.NodeID, current.AttemptCount)
		current.CurrentAttemptID = attemptID
		current.Status = spec.NodeStatusReady
		current.CurrentBottleneckLocation = "release_wait"
		current.StartedAt = &now
		return nil
	}); err != nil {
		return err
	}
	_ = r.registry.UpsertAttempt(context.Background(), spec.AttemptRecord{RunID: r.runID, NodeID: r.node.NodeID, AttemptID: attemptID, Status: spec.AttemptStatusPrepared, StartedAt: &now})
	appendEvent(context.Background(), r.registry, spec.EventRecord{RunID: r.runID, NodeID: r.node.NodeID, AttemptID: attemptID, Type: "node.ready", OccurredAt: now, Level: "info", Message: "node became ready for release"})
	if len(r.node.ArtifactBindings) > 0 {
		if err := validateResolvedBindingEnvKeys(r.node.ArtifactBindings); err != nil {
			_ = r.registry.UpsertAttempt(context.Background(), spec.AttemptRecord{
				RunID:                 r.runID,
				NodeID:                r.node.NodeID,
				AttemptID:             attemptID,
				Status:                spec.AttemptStatusErrored,
				StartedAt:             &now,
				FinishedAt:            timePtr(time.Now().UTC()),
				TerminalStopCause:     "failed",
				TerminalFailureReason: "input_env_key_collision",
			})
			return r.failNode(err, attemptID, "failed", "input_env_key_collision")
		}
		if err := r.registry.UpdateNode(context.Background(), r.runID, r.node.NodeID, func(current *spec.NodeRecord) error {
			current.Status = spec.NodeStatusBuildingBindings
			current.CurrentBottleneckLocation = "building_bindings"
			return nil
		}); err != nil {
			return err
		}
		appendEvent(context.Background(), r.registry, spec.EventRecord{RunID: r.runID, NodeID: r.node.NodeID, AttemptID: attemptID, Type: "node.building_bindings", OccurredAt: time.Now().UTC(), Level: "info", Message: "building artifact bindings"})
		if err := r.registry.UpdateNode(context.Background(), r.runID, r.node.NodeID, func(current *spec.NodeRecord) error {
			current.Status = spec.NodeStatusResolvingInputs
			current.CurrentBottleneckLocation = "resolving_inputs"
			return nil
		}); err != nil {
			return err
		}
		for _, binding := range r.node.ArtifactBindings {
			r.metrics.IncInputResolveRequests()
			req := handoff.ResolveBindingRequest{
				RunID:              r.runID,
				SampleRunID:        firstNonEmpty(run.Spec.Run.SampleRunID, r.runID),
				ChildNodeID:        r.node.NodeID,
				BindingName:        binding.BindingName,
				ChildInputName:     binding.ChildInputName,
				ProducerNodeID:     binding.ProducerNodeID,
				ProducerAttemptID:  r.lookupNodeAttemptID(ctx, binding.ProducerNodeID),
				ChildAttemptID:     attemptID,
				ProducerOutputName: binding.ProducerOutputName,
				ArtifactID:         binding.ArtifactID,
				ConsumePolicy:      binding.ConsumePolicy,
				ExpectedDigest:     binding.ExpectedDigest,
				Required:           binding.Required,
				TargetNodeName:     "",
			}
			resolved, err := r.resolveBindingForExecution(ctx, req, attemptID, binding)
			if err != nil {
				failureReason := resolveFailureReason(err)
				_ = r.registry.UpsertAttempt(context.Background(), spec.AttemptRecord{
					RunID:                 r.runID,
					NodeID:                r.node.NodeID,
					AttemptID:             attemptID,
					Status:                spec.AttemptStatusErrored,
					StartedAt:             &now,
					FinishedAt:            timePtr(time.Now().UTC()),
					TerminalStopCause:     "failed",
					TerminalFailureReason: failureReason,
				})
				return r.failNode(err, attemptID, "failed", failureReason)
			}
			appendEvent(context.Background(), r.registry, spec.EventRecord{
				RunID:      r.runID,
				NodeID:     r.node.NodeID,
				AttemptID:  attemptID,
				Type:       "node.input_resolved",
				OccurredAt: time.Now().UTC(),
				Level:      "info",
				Message:    fmt.Sprintf("binding=%s decision=%s status=%s", binding.BindingName, resolved.Decision, resolved.ResolutionStatus),
			})
			if resolved.Decision == "remote_fetch" {
				r.metrics.IncInputRemoteFetch()
			}
			if requiresMaterialization(resolved) {
				r.metrics.IncInputMaterializations()
			}
			r.recordLocalityHint(binding, resolved, attemptID)
			injectResolvedBindingEnv(&executionNode, binding, resolved)
		}
	}
	injectRuntimeContextEnv(&executionNode, run, attemptID)
	if err := applyPlacementHints(&executionNode, r.localityHints); err != nil {
		_ = r.registry.UpsertAttempt(context.Background(), spec.AttemptRecord{
			RunID:                 r.runID,
			NodeID:                r.node.NodeID,
			AttemptID:             attemptID,
			Status:                spec.AttemptStatusErrored,
			StartedAt:             &now,
			FinishedAt:            timePtr(time.Now().UTC()),
			TerminalStopCause:     "failed",
			TerminalFailureReason: "placement_conflict",
		})
		return r.failNode(err, attemptID, "failed", "placement_conflict")
	}
	recordPlacementHintApplication(context.Background(), r.registry, r.runID, r.node.NodeID, attemptID, r.localityHints, executionNode.Placement)
	if err := r.registry.UpdateNode(context.Background(), r.runID, r.node.NodeID, func(current *spec.NodeRecord) error {
		current.Status = spec.NodeStatusStarting
		current.CurrentBottleneckLocation = "backend_prepare"
		return nil
	}); err != nil {
		return err
	}
	appendEvent(context.Background(), r.registry, spec.EventRecord{RunID: r.runID, NodeID: r.node.NodeID, AttemptID: attemptID, Type: "node.starting", OccurredAt: time.Now().UTC(), Level: "info", Message: "backend prepare starting"})
	prepared, err := r.adapter.PrepareNode(ctx, run, executionNode)
	if err != nil {
		_ = r.registry.UpsertAttempt(context.Background(), spec.AttemptRecord{RunID: r.runID, NodeID: r.node.NodeID, AttemptID: attemptID, Status: spec.AttemptStatusErrored, StartedAt: &now, FinishedAt: timePtr(time.Now().UTC()), TerminalStopCause: "failed", TerminalFailureReason: "backend_prepare_error"})
		return r.failNode(err, attemptID, "failed", "backend_prepare_error")
	}
	if err := r.registry.UpdateNode(context.Background(), r.runID, r.node.NodeID, func(current *spec.NodeRecord) error {
		current.Status = spec.NodeStatusReleasing
		current.CurrentBottleneckLocation = "release_wait"
		return nil
	}); err != nil {
		return err
	}
	appendEvent(context.Background(), r.registry, spec.EventRecord{RunID: r.runID, NodeID: r.node.NodeID, AttemptID: attemptID, Type: "node.releasing", OccurredAt: time.Now().UTC(), Level: "info", Message: "bounded release waiting/start in progress"})
	releaseStartedAt := time.Now().UTC()
	handle, err := r.adapter.StartNode(ctx, prepared)
	releaseDelay := time.Since(releaseStartedAt)
	if err != nil {
		_ = r.registry.UpsertAttempt(context.Background(), spec.AttemptRecord{RunID: r.runID, NodeID: r.node.NodeID, AttemptID: attemptID, Status: spec.AttemptStatusErrored, StartedAt: &now, FinishedAt: timePtr(time.Now().UTC()), TerminalStopCause: "failed", TerminalFailureReason: "backend_start_error"})
		return r.failNode(err, attemptID, "failed", "backend_start_error")
	}
	r.metrics.IncJobsCreated()
	r.metrics.SetCleanupBacklogObjects(0)
	r.registerHandle(handle)
	defer r.unregisterHandle()
	startedAt := time.Now().UTC()
	if releaseDelay >= 200*time.Millisecond {
		appendEvent(context.Background(), r.registry, spec.EventRecord{RunID: r.runID, NodeID: r.node.NodeID, AttemptID: attemptID, Type: "node.release.waited", OccurredAt: startedAt, Level: "info", Message: fmt.Sprintf("bounded release delayed start by %s", releaseDelay.Truncate(10*time.Millisecond))})
	}
	_ = r.registry.UpsertAttempt(context.Background(), spec.AttemptRecord{RunID: r.runID, NodeID: r.node.NodeID, AttemptID: attemptID, Status: spec.AttemptStatusStarted, StartedAt: &startedAt})
	bottleneck := "running"
	if r.shouldObserveScheduling() {
		if kueueInfo, obsErr := r.adapter.ObserveNode(ctx, handle); obsErr == nil && kueueInfo != nil && kueueInfo.Observed {
			r.localityActualNode = kueueInfo.PodNodeName
			_ = r.registry.UpdateNode(context.Background(), r.runID, r.node.NodeID, func(current *spec.NodeRecord) error {
				current.Observation = spec.NodeObservation{
					KueueObserved:       kueueInfo.Observed,
					QueueName:           kueueInfo.QueueName,
					WorkloadName:        kueueInfo.WorkloadName,
					KueuePendingReason:  kueueInfo.PendingReason,
					KueueAdmitted:       kueueInfo.Admitted,
					PodName:             kueueInfo.PodName,
					PodNodeName:         kueueInfo.PodNodeName,
					PodScheduled:        kueueInfo.Scheduled,
					UnschedulableReason: kueueInfo.UnschedulableReason,
				}
				return nil
			})
			r.observeLocality(kueueInfo, attemptID)
			if !kueueInfo.Admitted {
				bottleneck = "kueue_pending"
				appendEvent(context.Background(), r.registry, spec.EventRecord{RunID: r.runID, NodeID: r.node.NodeID, AttemptID: attemptID, Type: "node.kueue.pending", OccurredAt: time.Now().UTC(), Level: "info", Message: firstNonEmpty(kueueInfo.PendingReason, "waiting for Kueue admission")})
			} else {
				appendEvent(context.Background(), r.registry, spec.EventRecord{RunID: r.runID, NodeID: r.node.NodeID, AttemptID: attemptID, Type: "node.kueue.admitted", OccurredAt: time.Now().UTC(), Level: "info", Message: firstNonEmpty(kueueInfo.WorkloadName, "Kueue admitted workload")})
				if !kueueInfo.Scheduled && kueueInfo.UnschedulableReason != "" {
					bottleneck = "scheduler_pending"
					appendEvent(context.Background(), r.registry, spec.EventRecord{RunID: r.runID, NodeID: r.node.NodeID, AttemptID: attemptID, Type: "node.scheduler.pending", OccurredAt: time.Now().UTC(), Level: "warn", Message: kueueInfo.UnschedulableReason})
					appendEvent(context.Background(), r.registry, spec.EventRecord{RunID: r.runID, NodeID: r.node.NodeID, AttemptID: attemptID, Type: "node.kueue.pod_observed", OccurredAt: time.Now().UTC(), Level: "info", Message: firstNonEmpty(kueueInfo.PodName, "pod observed")})
				}
			}
		}
	}
	if err := r.registry.UpdateNode(context.Background(), r.runID, r.node.NodeID, func(current *spec.NodeRecord) error {
		current.Status = spec.NodeStatusRunning
		current.CurrentBottleneckLocation = bottleneck
		return nil
	}); err != nil {
		return err
	}
	appendEvent(context.Background(), r.registry, spec.EventRecord{RunID: r.runID, NodeID: r.node.NodeID, AttemptID: attemptID, Type: "node.running", OccurredAt: startedAt, Level: "info", Message: "backend start completed and node is running"})
	result, err := r.adapter.WaitNode(ctx, handle)
	if err != nil {
		r.recordLocalityFallbackFailure(attemptID)
		if r.isRunCanceled() || result.TerminalStopCause == "canceled" {
			return r.cancelNode(attemptID, firstNonEmpty(result.TerminalFailureReason, "cancellation_requested"))
		}
		finishedAt := time.Now().UTC()
		_ = r.registry.UpsertAttempt(context.Background(), spec.AttemptRecord{RunID: r.runID, NodeID: r.node.NodeID, AttemptID: attemptID, Status: spec.AttemptStatusErrored, StartedAt: &startedAt, FinishedAt: &finishedAt, TerminalStopCause: firstNonEmpty(result.TerminalStopCause, "failed"), TerminalFailureReason: firstNonEmpty(result.TerminalFailureReason, "backend_wait_error")})
		return r.failNode(err, attemptID, firstNonEmpty(result.TerminalStopCause, "failed"), firstNonEmpty(result.TerminalFailureReason, "backend_wait_error"))
	}
	finishedAt := time.Now().UTC()
	r.recordLocalityFallbackSuccess(attemptID)
	if err := r.registerNodeOutputs(context.Background(), handle, attemptID, executionNode); err != nil {
		r.recordLocalityFallbackFailure(attemptID)
		_ = r.registry.UpsertAttempt(context.Background(), spec.AttemptRecord{
			RunID:                 r.runID,
			NodeID:                r.node.NodeID,
			AttemptID:             attemptID,
			Status:                spec.AttemptStatusErrored,
			StartedAt:             &startedAt,
			FinishedAt:            &finishedAt,
			TerminalStopCause:     "failed",
			TerminalFailureReason: "register_artifact_error",
		})
		return r.failNode(err, attemptID, "failed", "register_artifact_error")
	}
	if err := r.notifyNodeTerminal(context.Background(), "Succeeded", attemptID); err != nil {
		r.recordLocalityFallbackFailure(attemptID)
		_ = r.registry.UpsertAttempt(context.Background(), spec.AttemptRecord{
			RunID:                 r.runID,
			NodeID:                r.node.NodeID,
			AttemptID:             attemptID,
			Status:                spec.AttemptStatusErrored,
			StartedAt:             &startedAt,
			FinishedAt:            &finishedAt,
			TerminalStopCause:     "failed",
			TerminalFailureReason: "notify_node_terminal_error",
		})
		return r.failNode(err, attemptID, "failed", "notify_node_terminal_error")
	}
	_ = r.registry.UpsertAttempt(context.Background(), spec.AttemptRecord{
		RunID:                 r.runID,
		NodeID:                r.node.NodeID,
		AttemptID:             attemptID,
		Status:                spec.AttemptStatusCompleted,
		StartedAt:             &startedAt,
		FinishedAt:            &finishedAt,
		TerminalStopCause:     firstNonEmpty(result.TerminalStopCause, "finished"),
		TerminalFailureReason: result.TerminalFailureReason,
	})
	if err := r.registry.UpdateNode(context.Background(), r.runID, r.node.NodeID, func(current *spec.NodeRecord) error {
		current.Status = spec.NodeStatusSucceeded
		current.TerminalStopCause = firstNonEmpty(result.TerminalStopCause, "finished")
		current.TerminalFailureReason = result.TerminalFailureReason
		current.CurrentBottleneckLocation = ""
		current.FinishedAt = &finishedAt
		return nil
	}); err != nil {
		return err
	}
	appendEvent(context.Background(), r.registry, spec.EventRecord{RunID: r.runID, NodeID: r.node.NodeID, AttemptID: attemptID, Type: "node.succeeded", OccurredAt: finishedAt, Level: "info", StopCause: firstNonEmpty(result.TerminalStopCause, "finished")})
	return nil
}

func (r *nodeRunner) failNode(cause error, attemptID string, terminalStopCause string, failureReason string) error {
	r.recordLocalityFallbackFailure(attemptID)
	if r.isRunCanceled() {
		return r.cancelNode(attemptID, "cancellation_requested")
	}
	finishedAt := time.Now().UTC()
	appendEvent(context.Background(), r.registry, spec.EventRecord{RunID: r.runID, NodeID: r.node.NodeID, AttemptID: attemptID, Type: "node.failed", OccurredAt: finishedAt, Level: "error", StopCause: terminalStopCause, FailureReason: failureReason})
	_ = r.registry.UpdateNode(context.Background(), r.runID, r.node.NodeID, func(current *spec.NodeRecord) error {
		current.Status = spec.NodeStatusFailed
		current.TerminalStopCause = terminalStopCause
		current.TerminalFailureReason = failureReason
		current.CurrentBottleneckLocation = ""
		current.FinishedAt = &finishedAt
		return nil
	})
	if err := r.notifyNodeTerminal(context.Background(), "Failed", attemptID); err != nil {
		appendEvent(context.Background(), r.registry, spec.EventRecord{
			RunID:         r.runID,
			NodeID:        r.node.NodeID,
			AttemptID:     attemptID,
			Type:          "node.handoff.notify_failed",
			OccurredAt:    time.Now().UTC(),
			Level:         "error",
			StopCause:     "failed",
			FailureReason: "notify_node_terminal_error",
			Message:       err.Error(),
		})
	}
	return cause
}

func (r *nodeRunner) cancelNode(attemptID string, reason string) error {
	finishedAt := time.Now().UTC()
	terminalReason := r.effectiveCancellationReason(reason)
	_ = r.registry.UpsertAttempt(context.Background(), spec.AttemptRecord{RunID: r.runID, NodeID: r.node.NodeID, AttemptID: attemptID, Status: spec.AttemptStatusErrored, FinishedAt: &finishedAt, TerminalStopCause: "canceled", TerminalFailureReason: terminalReason})
	appendEvent(context.Background(), r.registry, spec.EventRecord{RunID: r.runID, NodeID: r.node.NodeID, AttemptID: attemptID, Type: "node.canceled", OccurredAt: finishedAt, Level: "warn", StopCause: "canceled", FailureReason: terminalReason})
	if err := r.registry.UpdateNode(context.Background(), r.runID, r.node.NodeID, func(current *spec.NodeRecord) error {
		current.Status = spec.NodeStatusCanceled
		current.TerminalStopCause = "canceled"
		current.TerminalFailureReason = terminalReason
		current.CurrentBottleneckLocation = ""
		current.FinishedAt = &finishedAt
		return nil
	}); err != nil {
		return err
	}
	if err := r.notifyNodeTerminal(context.Background(), "Canceled", attemptID); err != nil {
		appendEvent(context.Background(), r.registry, spec.EventRecord{
			RunID:         r.runID,
			NodeID:        r.node.NodeID,
			AttemptID:     attemptID,
			Type:          "node.handoff.notify_failed",
			OccurredAt:    time.Now().UTC(),
			Level:         "error",
			StopCause:     "canceled",
			FailureReason: "notify_node_terminal_error",
			Message:       err.Error(),
		})
	}
	return nil
}

func (r *nodeRunner) shouldObserveScheduling() bool {
	return (r.node.Kueue != nil && r.node.Kueue.QueueName != "") || len(r.localityHints) > 0
}

func (r *nodeRunner) recordLocalityHint(binding spec.ArtifactBinding, resolved handoff.ResolveBindingResponse, attemptID string) {
	if resolved.PlacementIntent.NodeName == "" {
		return
	}
	hint := bindingLocalityHint{
		BindingName:             binding.BindingName,
		ChildInputName:          binding.ChildInputName,
		PreferredNodeName:       resolved.PlacementIntent.NodeName,
		PlacementMode:           resolved.PlacementIntent.Mode,
		Decision:                resolved.Decision,
		RequiresMaterialization: requiresMaterialization(resolved),
	}
	r.localityHints = append(r.localityHints, hint)
	r.metrics.IncLocalityPreferred()
	appendEvent(context.Background(), r.registry, spec.EventRecord{
		RunID:      r.runID,
		NodeID:     r.node.NodeID,
		AttemptID:  attemptID,
		Type:       "node.locality.preferred",
		OccurredAt: time.Now().UTC(),
		Level:      "info",
		Message:    fmt.Sprintf("binding=%s preferredNode=%s mode=%s", firstNonEmpty(binding.ChildInputName, binding.BindingName), hint.PreferredNodeName, firstNonEmpty(hint.PlacementMode, "unspecified")),
	})
}

func (r *nodeRunner) observeLocality(info *backend.OptionalKueueInfo, attemptID string) {
	if info == nil || info.PodNodeName == "" {
		return
	}
	for _, hint := range r.localityHints {
		bindingName := firstNonEmpty(hint.ChildInputName, hint.BindingName)
		if info.PodNodeName == hint.PreferredNodeName {
			r.metrics.IncLocalityMatched()
			appendEvent(context.Background(), r.registry, spec.EventRecord{
				RunID:      r.runID,
				NodeID:     r.node.NodeID,
				AttemptID:  attemptID,
				Type:       "node.locality.matched",
				OccurredAt: time.Now().UTC(),
				Level:      "info",
				Message:    fmt.Sprintf("binding=%s podNode=%s preferredNode=%s", bindingName, info.PodNodeName, hint.PreferredNodeName),
			})
			continue
		}
		r.metrics.IncLocalityMiss()
		appendEvent(context.Background(), r.registry, spec.EventRecord{
			RunID:      r.runID,
			NodeID:     r.node.NodeID,
			AttemptID:  attemptID,
			Type:       "node.locality.missed",
			OccurredAt: time.Now().UTC(),
			Level:      "warn",
			Message:    fmt.Sprintf("binding=%s podNode=%s preferredNode=%s", bindingName, info.PodNodeName, hint.PreferredNodeName),
		})
		if hint.Decision == "remote_fetch" || hint.RequiresMaterialization {
			r.localityFallbackStarted = true
			r.metrics.IncLocalityFallbackStart()
			appendEvent(context.Background(), r.registry, spec.EventRecord{
				RunID:      r.runID,
				NodeID:     r.node.NodeID,
				AttemptID:  attemptID,
				Type:       "node.locality.fallback_started",
				OccurredAt: time.Now().UTC(),
				Level:      "info",
				Message:    fmt.Sprintf("binding=%s decision=%s materialization=%t", bindingName, hint.Decision, hint.RequiresMaterialization),
			})
		}
	}
}

func (r *nodeRunner) recordLocalityFallbackSuccess(attemptID string) {
	if !r.localityFallbackStarted {
		return
	}
	r.metrics.IncLocalityFallbackOK()
	appendEvent(context.Background(), r.registry, spec.EventRecord{
		RunID:      r.runID,
		NodeID:     r.node.NodeID,
		AttemptID:  attemptID,
		Type:       "node.locality.fallback_succeeded",
		OccurredAt: time.Now().UTC(),
		Level:      "info",
		Message:    firstNonEmpty(r.localityActualNode, "fallback preserved node success"),
	})
	r.localityFallbackStarted = false
}

func (r *nodeRunner) recordLocalityFallbackFailure(attemptID string) {
	if !r.localityFallbackStarted {
		return
	}
	r.metrics.IncLocalityFallbackFail()
	appendEvent(context.Background(), r.registry, spec.EventRecord{
		RunID:      r.runID,
		NodeID:     r.node.NodeID,
		AttemptID:  attemptID,
		Type:       "node.locality.fallback_failed",
		OccurredAt: time.Now().UTC(),
		Level:      "error",
		Message:    firstNonEmpty(r.localityActualNode, "fallback path failed"),
	})
	r.localityFallbackStarted = false
}

func (r *nodeRunner) effectiveCancellationReason(reason string) string {
	if reason != "" && reason != "cancellation_requested" {
		return reason
	}
	run, err := r.registry.GetRun(context.Background(), r.runID)
	if err == nil {
		if run.TerminalFailureReason != "" && run.TerminalFailureReason != "cancellation_requested" {
			return run.TerminalFailureReason
		}
	}
	return firstNonEmpty(reason, "cancellation_requested")
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

func (r *nodeRunner) notifyNodeTerminal(ctx context.Context, terminalState string, attemptID string) error {
	return r.handoff.NotifyNodeTerminal(ctx, handoff.NotifyNodeTerminalRequest{
		SampleRunID:   firstNonEmpty(r.sampleRunID(ctx), r.runID),
		NodeID:        r.node.NodeID,
		AttemptID:     attemptID,
		TerminalState: terminalState,
	})
}

func (r *nodeRunner) registerNodeOutputs(ctx context.Context, handle backend.Handle, attemptID string, node spec.Node) error {
	if len(node.Outputs) == 0 {
		return nil
	}
	outputMetadata, err := r.collectOutputMetadata(ctx, handle, node)
	if err != nil {
		return err
	}
	sampleRunID := firstNonEmpty(r.sampleRunID(ctx), r.runID)
	for _, outputName := range node.Outputs {
		if outputName == "" {
			continue
		}
		metadata, ok := outputMetadata[outputName]
		if !ok {
			return fmt.Errorf("required output %s missing from manifest for node %s", outputName, r.node.NodeID)
		}
		if strings.TrimSpace(metadata.Digest) == "" {
			return fmt.Errorf("required output %s has empty digest for node %s", outputName, r.node.NodeID)
		}
		if strings.TrimSpace(metadata.URI) == "" && len(metadata.Locations) == 0 {
			return fmt.Errorf("required output %s has neither uri nor locations for node %s", outputName, r.node.NodeID)
		}
		if err := r.handoff.RegisterArtifact(ctx, handoff.RegisterArtifactRequest{
			SampleRunID:       sampleRunID,
			ProducerNodeID:    r.node.NodeID,
			ProducerAttemptID: attemptID,
			OutputName:        outputName,
			ArtifactID:        fmt.Sprintf("%s/%s/%s/%s", sampleRunID, r.node.NodeID, attemptID, outputName),
			Digest:            metadata.Digest,
			NodeName:          metadata.NodeName,
			URI:               metadata.URI,
			LogicalURI:        metadata.LogicalURI,
			Locations:         toHandoffLocations(metadata.Locations, metadata.NodeName),
			SizeBytes:         metadata.SizeBytes,
		}); err != nil {
			return fmt.Errorf("register artifact %s for node %s: %w", outputName, r.node.NodeID, err)
		}
		r.metrics.IncArtifactsRegistered()
	}
	return nil
}

func (r *nodeRunner) collectOutputMetadata(ctx context.Context, handle backend.Handle, node spec.Node) (map[string]backend.OutputMetadata, error) {
	provider, ok := r.adapter.(backend.OutputMetadataProvider)
	if !ok {
		if len(node.Outputs) > 0 {
			return nil, fmt.Errorf("output metadata provider required for declared outputs on node %s", r.node.NodeID)
		}
		return nil, nil
	}
	metadata, err := provider.CollectOutputMetadata(ctx, handle, node)
	if err != nil {
		return nil, fmt.Errorf("collect output metadata for node %s: %w", r.node.NodeID, err)
	}
	return metadata, nil
}

func toHandoffLocations(locations []provenance.ArtifactLocation, fallbackNodeName string) []handoff.ArtifactLocation {
	if len(locations) == 0 {
		return nil
	}
	out := make([]handoff.ArtifactLocation, 0, len(locations))
	for _, loc := range locations {
		var converted handoff.ArtifactLocation
		if loc.NodeLocal != nil {
			nodeName := loc.NodeLocal.NodeName
			if nodeName == "" {
				nodeName = fallbackNodeName
			}
			converted.NodeLocal = &handoff.NodeLocalLocation{
				NodeName: nodeName,
				Path:     loc.NodeLocal.Path,
			}
		}
		out = append(out, converted)
	}
	return out
}

func cloneNode(node spec.Node) spec.Node {
	cloned := node
	if node.Env != nil {
		cloned.Env = make(map[string]string, len(node.Env))
		for k, v := range node.Env {
			cloned.Env[k] = v
		}
	}
	if node.Metadata != nil {
		cloned.Metadata = make(map[string]string, len(node.Metadata))
		for k, v := range node.Metadata {
			cloned.Metadata[k] = v
		}
	}
	if node.Placement != nil {
		cloned.Placement = &spec.PlacementHints{}
		if node.Placement.NodeSelector != nil {
			cloned.Placement.NodeSelector = make(map[string]string, len(node.Placement.NodeSelector))
			for k, v := range node.Placement.NodeSelector {
				cloned.Placement.NodeSelector[k] = v
			}
		}
	}
	return cloned
}

func injectResolvedBindingEnv(node *spec.Node, binding spec.ArtifactBinding, resolved handoff.ResolveBindingResponse) {
	if node.Env == nil {
		node.Env = make(map[string]string)
	}
	keyBase := sanitizeEnvSegment(firstNonEmpty(binding.ChildInputName, binding.BindingName, binding.ProducerOutputName))
	node.Env["JUMI_INPUT_"+keyBase+"_STATUS"] = resolved.ResolutionStatus
	node.Env["JUMI_INPUT_"+keyBase+"_DECISION"] = resolved.Decision
	node.Env["JUMI_INPUT_"+keyBase+"_URI"] = resolved.MaterializationPlan.URI
	node.Env["JUMI_INPUT_"+keyBase+"_SOURCE_NODE"] = resolved.PlacementIntent.NodeName
	node.Env["JUMI_INPUT_"+keyBase+"_PLACEMENT_MODE"] = resolved.PlacementIntent.Mode
	node.Env["JUMI_INPUT_"+keyBase+"_MATERIALIZATION_MODE"] = resolved.MaterializationPlan.Mode
	node.Env["JUMI_INPUT_"+keyBase+"_EXPECTED_DIGEST"] = resolved.MaterializationPlan.ExpectedDigest
	if resolved.MaterializationPlan.ExpectedSize > 0 {
		node.Env["JUMI_INPUT_"+keyBase+"_EXPECTED_SIZE_BYTES"] = strconv.FormatInt(resolved.MaterializationPlan.ExpectedSize, 10)
	}
	if resolved.MaterializationPlan.SourceLocation != nil && resolved.MaterializationPlan.SourceLocation.NodeLocal != nil {
		node.Env["JUMI_INPUT_"+keyBase+"_NODE_LOCAL_PATH"] = resolved.MaterializationPlan.SourceLocation.NodeLocal.Path
	}
	if safeLocalPath := sanitizeResolvedLocalPath(resolved.MaterializationPlan.LocalPath); safeLocalPath != "" {
		node.Env["JUMI_INPUT_"+keyBase+"_LOCAL_PATH"] = safeLocalPath
	}
	if requiresMaterialization(resolved) {
		node.Env["JUMI_INPUT_"+keyBase+"_REQUIRES_MATERIALIZATION"] = "true"
	} else {
		node.Env["JUMI_INPUT_"+keyBase+"_REQUIRES_MATERIALIZATION"] = "false"
	}
}

func injectRuntimeContextEnv(node *spec.Node, run spec.RunRecord, attemptID string) {
	if node.Env == nil {
		node.Env = make(map[string]string)
	}
	node.Env["JUMI_RUN_ID"] = run.RunID
	node.Env["JUMI_NODE_ID"] = node.NodeID
	node.Env["JUMI_ATTEMPT_ID"] = attemptID
	node.Env["JUMI_OUTPUT_ROOT"] = "/out"
	if sampleRunID := run.Spec.Run.SampleRunID; sampleRunID != "" {
		node.Env["JUMI_SAMPLE_RUN_ID"] = sampleRunID
	}
	if len(node.Outputs) > 0 {
		node.Env["JUMI_OUTPUT_MANIFEST_PATH"] = provenance.AttemptArtifactsManifestPath(run.RunID, node.NodeID, attemptID)
	}
}

func applyPlacementHints(node *spec.Node, hints []bindingLocalityHint) error {
	if node == nil || len(hints) == 0 {
		return nil
	}
	requiredNode := ""
	hasPreferred := false
	for _, hint := range hints {
		switch hint.PlacementMode {
		case "required_node":
			if strings.TrimSpace(hint.PreferredNodeName) == "" {
				continue
			}
			if requiredNode == "" {
				requiredNode = hint.PreferredNodeName
				continue
			}
			if requiredNode != hint.PreferredNodeName {
				return fmt.Errorf("conflicting required_node placement intents: %s vs %s", requiredNode, hint.PreferredNodeName)
			}
		case "preferred_node":
			if strings.TrimSpace(hint.PreferredNodeName) != "" {
				hasPreferred = true
			}
		}
	}
	if requiredNode == "" && !hasPreferred {
		return nil
	}
	if node.Placement == nil {
		node.Placement = &spec.PlacementHints{}
	}
	if requiredNode != "" {
		if len(node.Placement.PreferredNodes) > 0 {
			return fmt.Errorf("preferred placement already set while applying required_node=%s", requiredNode)
		}
		if node.Placement.NodeSelector == nil {
			node.Placement.NodeSelector = map[string]string{}
		}
		if existing := strings.TrimSpace(node.Placement.NodeSelector[hostnameNodeSelectorKey]); existing != "" && existing != requiredNode {
			return fmt.Errorf("node selector %s=%s conflicts with required_node=%s", hostnameNodeSelectorKey, existing, requiredNode)
		}
		node.Placement.NodeSelector[hostnameNodeSelectorKey] = requiredNode
		node.Placement.RequiredNodeName = requiredNode
		return nil
	}
	if node.Placement.RequiredNodeName != "" {
		return fmt.Errorf("required placement already set while applying preferred nodes")
	}
	preferred := make([]spec.WeightedNodePreference, 0, len(hints))
	seen := map[string]struct{}{}
	for _, hint := range hints {
		if hint.PlacementMode != "preferred_node" || strings.TrimSpace(hint.PreferredNodeName) == "" {
			continue
		}
		if _, ok := seen[hint.PreferredNodeName]; ok {
			continue
		}
		seen[hint.PreferredNodeName] = struct{}{}
		preferred = append(preferred, spec.WeightedNodePreference{
			NodeName: hint.PreferredNodeName,
			Weight:   100,
		})
	}
	if len(preferred) > 0 {
		node.Placement.PreferredNodes = preferred
	}
	return nil
}

func recordPlacementHintApplication(ctx context.Context, reg registry.Registry, runID, nodeID, attemptID string, hints []bindingLocalityHint, placement *spec.PlacementHints) {
	if len(hints) == 0 {
		return
	}
	appliedRequired := ""
	if placement != nil && placement.NodeSelector != nil {
		appliedRequired = strings.TrimSpace(placement.NodeSelector[hostnameNodeSelectorKey])
	}
	for _, hint := range hints {
		bindingName := firstNonEmpty(hint.ChildInputName, hint.BindingName)
		switch hint.PlacementMode {
		case "required_node":
			if appliedRequired == hint.PreferredNodeName && appliedRequired != "" {
				appendEvent(ctx, reg, spec.EventRecord{
					RunID:      runID,
					NodeID:     nodeID,
					AttemptID:  attemptID,
					Type:       "node.placement.required_applied",
					OccurredAt: time.Now().UTC(),
					Level:      "info",
					Message:    fmt.Sprintf("binding=%s nodeSelector[%s]=%s", bindingName, hostnameNodeSelectorKey, appliedRequired),
				})
			}
		case "preferred_node":
			appendEvent(ctx, reg, spec.EventRecord{
				RunID:      runID,
				NodeID:     nodeID,
				AttemptID:  attemptID,
				Type:       "node.placement.preferred_applied",
				OccurredAt: time.Now().UTC(),
				Level:      "info",
				Message:    fmt.Sprintf("binding=%s preferredNode=%s mapped to backend preferred placement", bindingName, hint.PreferredNodeName),
			})
		}
	}
}

func requiresMaterialization(resolved handoff.ResolveBindingResponse) bool {
	mode := strings.TrimSpace(resolved.MaterializationPlan.Mode)
	return mode != "" && !strings.EqualFold(mode, "none")
}

func missingBindingFailureReason(resolved handoff.ResolveBindingResponse) string {
	if resolved.Decision == "producer_failed" {
		return "input_resolution_producer_failed"
	}
	return "input_resolution_missing"
}

func resolveBindingFailureReason(binding spec.ArtifactBinding, resolved handoff.ResolveBindingResponse) string {
	switch strings.ToUpper(strings.TrimSpace(resolved.ResolutionStatus)) {
	case "RESOLVED":
		return ""
	case "PENDING":
		if resolved.Retryable {
			return "input_resolution_pending"
		}
		return "input_resolution_pending_not_retryable"
	case "MISSING":
		if !binding.Required {
			return ""
		}
		return missingBindingFailureReason(resolved)
	case "PRODUCER_FAILED":
		return "input_resolution_producer_failed"
	case "POLICY_BLOCKED":
		return "input_resolution_policy_blocked"
	case "DIGEST_MISMATCH":
		return "input_resolution_digest_mismatch"
	case "GC_EXPIRED":
		return "input_resolution_gc_expired"
	case "UNAVAILABLE":
		if resolved.Retryable {
			return "input_resolution_unavailable_retryable"
		}
		return "input_resolution_unavailable"
	default:
		return "input_resolution_unknown_status"
	}
}

func classifyResolvedBinding(binding spec.ArtifactBinding, resolved handoff.ResolveBindingResponse) (resolveAction, string) {
	switch strings.ToUpper(strings.TrimSpace(resolved.ResolutionStatus)) {
	case "", "RESOLVED":
		return resolveProceed, ""
	case "PENDING":
		if resolved.Retryable {
			return resolveRetry, "input_resolution_pending"
		}
		return resolveFail, "input_resolution_pending_not_retryable"
	case "MISSING":
		if binding.Required {
			return resolveFail, missingBindingFailureReason(resolved)
		}
		return resolveSkipOptional, ""
	case "PRODUCER_FAILED":
		return resolveFail, "input_resolution_producer_failed"
	case "POLICY_BLOCKED":
		return resolveFail, "input_resolution_policy_blocked"
	case "DIGEST_MISMATCH":
		return resolveFail, "input_resolution_digest_mismatch"
	case "GC_EXPIRED":
		return resolveFail, "input_resolution_gc_expired"
	case "UNAVAILABLE":
		if resolved.Retryable {
			return resolveRetry, "input_resolution_unavailable_retryable"
		}
		return resolveFail, "input_resolution_unavailable"
	default:
		return resolveFail, "input_resolution_unknown_status"
	}
}

func resolveFailureReason(err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	if strings.HasPrefix(msg, "input_resolution_") {
		return msg
	}
	return "resolve_handoff_error"
}

func sanitizeEnvSegment(value string) string {
	if value == "" {
		return "UNSPECIFIED"
	}
	var b strings.Builder
	prevUnderscore := false
	for _, r := range value {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(unicode.ToUpper(r))
			prevUnderscore = false
		default:
			if !prevUnderscore {
				b.WriteByte('_')
				prevUnderscore = true
			}
		}
	}
	result := strings.Trim(b.String(), "_")
	if result == "" {
		return "UNSPECIFIED"
	}
	return result
}

func validateResolvedBindingEnvKeys(bindings []spec.ArtifactBinding) error {
	seen := make(map[string]string, len(bindings))
	for _, binding := range bindings {
		original := firstNonEmpty(binding.ChildInputName, binding.BindingName, binding.ProducerOutputName)
		key := sanitizeEnvSegment(original)
		if existing, ok := seen[key]; ok {
			return fmt.Errorf("input env key collision: %q and %q both sanitize to %q", existing, original, key)
		}
		seen[key] = original
	}
	return nil
}

func sanitizeResolvedLocalPath(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ""
	}
	if filepath.IsAbs(trimmed) || !filepath.IsLocal(trimmed) {
		return ""
	}
	cleaned := filepath.Clean(trimmed)
	rel, err := filepath.Rel("inputs", cleaned)
	if err != nil {
		return ""
	}
	if rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return ""
	}
	return cleaned
}

func (r *nodeRunner) sampleRunID(ctx context.Context) string {
	run, err := r.registry.GetRun(ctx, r.runID)
	if err != nil {
		return ""
	}
	return run.Spec.Run.SampleRunID
}

func (r *nodeRunner) lookupNodeAttemptID(ctx context.Context, nodeID string) string {
	nodes, err := r.registry.ListNodes(ctx, r.runID)
	if err != nil {
		return ""
	}
	for _, node := range nodes {
		if node.NodeID == nodeID {
			return node.CurrentAttemptID
		}
	}
	return ""
}

func (r *nodeRunner) resolveBindingWithRetry(ctx context.Context, req handoff.ResolveBindingRequest, attemptID string, bindingName string) (handoff.ResolveBindingResponse, error) {
	var lastErr error
	for attempt := 1; attempt <= resolveBindingMaxAttempts; attempt++ {
		resolved, err := r.handoff.ResolveBinding(ctx, req)
		if err == nil {
			return resolved, nil
		}
		lastErr = err
		if !isTransientResolveBindingError(err) {
			return handoff.ResolveBindingResponse{}, err
		}
		appendEvent(context.Background(), r.registry, spec.EventRecord{
			RunID:         r.runID,
			NodeID:        r.node.NodeID,
			AttemptID:     attemptID,
			Type:          "node.input_resolve_retry",
			OccurredAt:    time.Now().UTC(),
			Level:         "warn",
			FailureReason: "resolve_handoff_error",
			Message:       fmt.Sprintf("binding=%s attempt=%d/%d error=%s", bindingName, attempt, resolveBindingMaxAttempts, err.Error()),
		})
		if attempt == resolveBindingMaxAttempts {
			break
		}
		select {
		case <-ctx.Done():
			return handoff.ResolveBindingResponse{}, ctx.Err()
		case <-time.After(resolveBindingRetryDelay):
		}
	}
	return handoff.ResolveBindingResponse{}, lastErr
}

func (r *nodeRunner) resolveBindingForExecution(ctx context.Context, req handoff.ResolveBindingRequest, attemptID string, binding spec.ArtifactBinding) (handoff.ResolveBindingResponse, error) {
	var lastResolved handoff.ResolveBindingResponse
	pendingDelay := resolveBindingPendingInitialDelay
	for attempt := 1; attempt <= resolveBindingPendingMaxAttempts; attempt++ {
		resolved, err := r.resolveBindingWithRetry(ctx, req, attemptID, binding.BindingName)
		if err != nil {
			return handoff.ResolveBindingResponse{}, err
		}
		lastResolved = resolved
		action, failureReason := classifyResolvedBinding(binding, resolved)
		switch action {
		case resolveProceed, resolveSkipOptional:
			return resolved, nil
		case resolveFail:
			return handoff.ResolveBindingResponse{}, fmt.Errorf("%s", failureReason)
		case resolveRetry:
			appendEvent(context.Background(), r.registry, spec.EventRecord{
				RunID:         r.runID,
				NodeID:        r.node.NodeID,
				AttemptID:     attemptID,
				Type:          "node.input_resolve_pending",
				OccurredAt:    time.Now().UTC(),
				Level:         "warn",
				FailureReason: failureReason,
				Message:       fmt.Sprintf("binding=%s status=%s attempt=%d/%d nextDelay=%s", binding.BindingName, resolved.ResolutionStatus, attempt, resolveBindingPendingMaxAttempts, pendingDelay),
			})
			if attempt == resolveBindingPendingMaxAttempts {
				return handoff.ResolveBindingResponse{}, fmt.Errorf("%s", failureReason)
			}
			select {
			case <-ctx.Done():
				return handoff.ResolveBindingResponse{}, ctx.Err()
			case <-time.After(pendingDelay):
			}
			pendingDelay *= resolveBindingPendingBackoffFactor
			if pendingDelay > resolveBindingPendingMaxDelay {
				pendingDelay = resolveBindingPendingMaxDelay
			}
		}
	}
	return handoff.ResolveBindingResponse{}, fmt.Errorf("%s", resolveBindingFailureReason(binding, lastResolved))
}

func isTransientResolveBindingError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		return true
	}
	switch status.Code(err) {
	case codes.Unavailable, codes.DeadlineExceeded, codes.ResourceExhausted, codes.Aborted, codes.Internal:
		return true
	case codes.InvalidArgument, codes.NotFound, codes.FailedPrecondition, codes.PermissionDenied, codes.Unauthenticated, codes.Unimplemented:
		return false
	}
	if httpStatus, ok := handoffHTTPStatus(err); ok {
		switch httpStatus {
		case 408, 425, 429, 500, 502, 503, 504:
			return true
		default:
			return false
		}
	}
	return false
}

func handoffHTTPStatus(err error) (int, bool) {
	msg := err.Error()
	const marker = "handoff resolve failed with status "
	idx := strings.Index(msg, marker)
	if idx < 0 {
		return 0, false
	}
	codeText := msg[idx+len(marker):]
	end := strings.IndexFunc(codeText, func(r rune) bool { return r < '0' || r > '9' })
	if end >= 0 {
		codeText = codeText[:end]
	}
	if codeText == "" {
		return 0, false
	}
	code, convErr := strconv.Atoi(codeText)
	if convErr != nil {
		return 0, false
	}
	return code, true
}

func artifactOutputURI(runID, nodeID, outputName string) string {
	return fmt.Sprintf("jumi://runs/%s/nodes/%s/outputs/%s",
		url.PathEscape(runID),
		url.PathEscape(nodeID),
		url.PathEscape(outputName),
	)
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
	appendEvent(ctx, reg, spec.EventRecord{RunID: runID, Type: "run.canceled", OccurredAt: now, Level: "warn", StopCause: "canceled", FailureReason: "cancellation_requested"})
	nodes, err := reg.ListNodes(ctx, runID)
	if err != nil {
		return err
	}
	for _, node := range nodes {
		nodeID := node.NodeID
		canceled := false
		_ = reg.UpdateNode(ctx, runID, nodeID, func(current *spec.NodeRecord) error {
			switch current.Status {
			case spec.NodeStatusSucceeded, spec.NodeStatusFailed, spec.NodeStatusCanceled, spec.NodeStatusSkipped:
				return nil
			default:
				current.Status = spec.NodeStatusCanceled
				current.TerminalStopCause = "canceled"
				current.TerminalFailureReason = "cancellation_requested"
				current.FinishedAt = &now
				canceled = true
				return nil
			}
		})
		if canceled {
			appendEvent(ctx, reg, spec.EventRecord{RunID: runID, NodeID: nodeID, Type: "node.canceled", OccurredAt: now, Level: "warn", StopCause: "canceled", FailureReason: "cancellation_requested"})
		}
	}
	return nil
}

func appendEvent(ctx context.Context, reg registry.Registry, event spec.EventRecord) {
	_ = reg.AppendEvent(ctx, event)
}

func eventLevelForRunStatus(runStatus spec.RunStatus) string {
	switch runStatus {
	case spec.RunStatusSucceeded:
		return "info"
	case spec.RunStatusCanceled:
		return "warn"
	default:
		return "error"
	}
}

func timePtr(v time.Time) *time.Time {
	return &v
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
