package executor

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/HeaInSeo/JUMI/pkg/handoff"
	"github.com/HeaInSeo/JUMI/pkg/registry"
	"github.com/HeaInSeo/JUMI/pkg/spec"
)

// statusRecordingRegistry wraps a real Registry and records every NodeStatus
// value observed for one specific node, in the order UpdateNode's mutation
// closures actually set them - as opposed to inferring status transitions
// from which events were emitted, which can diverge from the status values
// a caller (or a future refactor) actually persists.
type statusRecordingRegistry struct {
	registry.Registry
	nodeID string

	mu       sync.Mutex
	statuses []spec.NodeStatus
}

func (s *statusRecordingRegistry) UpdateNode(ctx context.Context, runID, nodeID string, update func(*spec.NodeRecord) error) error {
	if nodeID != s.nodeID {
		return s.Registry.UpdateNode(ctx, runID, nodeID, update)
	}
	wrapped := func(current *spec.NodeRecord) error {
		if err := update(current); err != nil {
			return err
		}
		s.mu.Lock()
		s.statuses = append(s.statuses, current.Status)
		s.mu.Unlock()
		return nil
	}
	return s.Registry.UpdateNode(ctx, runID, nodeID, wrapped)
}

func (s *statusRecordingRegistry) recordedStatuses() []spec.NodeStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]spec.NodeStatus, len(s.statuses))
	copy(out, s.statuses)
	return out
}

// TestDagEngineOrderedStateTransitionSequence asserts the full intended
// node lifecycle sequence (Ready -> BuildingBindings -> ResolvingInputs ->
// Starting -> Releasing -> Running -> Succeeded) actually happens in that
// order, by recording the actual NodeStatus values passed to
// Registry.UpdateNode via statusRecordingRegistry - not by inferring the
// sequence from emitted event types, which don't necessarily correspond
// 1:1 with status transitions (e.g. node.input_resolved is emitted after
// resolving a binding regardless of whether NodeStatusResolvingInputs was
// ever actually set). Node "b" has an artifact binding on "a" specifically
// so BuildingBindings/ResolvingInputs (which only run when ArtifactBindings
// is non-empty) are exercised.
func TestDagEngineOrderedStateTransitionSequence(t *testing.T) {
	base := registry.NewMemoryRegistry()
	reg := &statusRecordingRegistry{Registry: base, nodeID: "b"}
	adapter := &fakeAdapter{failOn: map[string]bool{}}
	handoffClient := &fakeHandoffClient{
		response: handoff.ResolveBindingResponse{
			ResolutionStatus: "RESOLVED",
			Decision:         "remote_fetch",
			PlacementIntent:  handoff.PlacementIntent{Mode: "required_node", NodeName: "node-a"},
			MaterializationPlan: handoff.MaterializationPlan{
				Mode: "remote_fetch", URI: "http://artifact.local/output", ExpectedDigest: "sha256:abc",
			},
		},
	}
	engine := NewDagEngineWithHandoff(reg, adapter, handoffClient)
	specInput := spec.ExecutableRunSpec{
		Run: spec.RunMetadata{RunID: "run-order", SampleRunID: "sample-order", SubmittedAt: time.Now().UTC(), FailurePolicy: spec.FailurePolicy{Mode: "fail-fast"}},
		Graph: spec.Graph{
			Nodes: []spec.Node{
				{NodeID: "a", Image: "busybox:1.36"},
				{NodeID: "b", Image: "busybox:1.36", ArtifactBindings: []spec.ArtifactBinding{{
					BindingName:        "dataset",
					ChildInputName:     "dataset",
					ProducerNodeID:     "a",
					ProducerOutputName: "output",
					ArtifactID:         "sample-order:a:output",
					ConsumePolicy:      "RemoteOK",
					Required:           true,
				}}},
			},
			Edges: [][]string{{"a", "b"}},
		},
	}
	record := spec.RunRecord{RunID: specInput.Run.RunID, Status: spec.RunStatusAccepted, AcceptedAt: time.Now().UTC(), Spec: specInput}
	nodes := []spec.NodeRecord{{RunID: record.RunID, NodeID: "a", Status: spec.NodeStatusPending}, {RunID: record.RunID, NodeID: "b", Status: spec.NodeStatusPending}}
	if err := reg.CreateRun(context.Background(), record, nodes); err != nil {
		t.Fatalf("CreateRun() error = %v", err)
	}
	if err := engine.Admit(context.Background(), record); err != nil {
		t.Fatalf("Admit() error = %v", err)
	}
	waitForRunStatus(t, reg, record.RunID, spec.RunStatusSucceeded)

	gotStatuses := reg.recordedStatuses()
	wantStatuses := []spec.NodeStatus{
		spec.NodeStatusReady,
		spec.NodeStatusBuildingBindings,
		spec.NodeStatusResolvingInputs,
		spec.NodeStatusStarting,
		spec.NodeStatusReleasing,
		spec.NodeStatusRunning,
		spec.NodeStatusSucceeded,
	}
	if len(gotStatuses) != len(wantStatuses) {
		t.Fatalf("node b UpdateNode status sequence = %v, want exactly %v", gotStatuses, wantStatuses)
	}
	for i, want := range wantStatuses {
		if gotStatuses[i] != want {
			t.Fatalf("node b UpdateNode status sequence = %v, want exactly %v", gotStatuses, wantStatuses)
		}
	}

	// Corroborate with the emitted events too, as a secondary check that
	// the two views of the lifecycle stay consistent with each other.
	events, err := base.ListEvents(context.Background(), record.RunID, 0)
	if err != nil {
		t.Fatalf("ListEvents() error = %v", err)
	}
	var seq []string
	for _, ev := range events {
		if ev.NodeID == "b" {
			seq = append(seq, ev.Type)
		}
	}
	wantEvents := []string{"node.ready", "node.building_bindings", "node.input_resolved", "node.starting", "node.releasing", "node.running", "node.succeeded"}
	idx := 0
	for _, evType := range seq {
		if idx < len(wantEvents) && evType == wantEvents[idx] {
			idx++
		}
	}
	if idx != len(wantEvents) {
		t.Fatalf("event sequence for node b did not contain %v in order, got %v (matched %d/%d)", wantEvents, seq, idx, len(wantEvents))
	}
}

// TestDagEngineCancelIsNoOpOnAlreadyTerminalRunAndNode exercises the
// terminal-state guards in Cancel (executor.go) that were never directly
// tested: calling Cancel on a run/node already in a terminal state must not
// overwrite that terminal state, and must not emit a spurious
// "node.canceled" event for an already-terminal node.
func TestDagEngineCancelIsNoOpOnAlreadyTerminalRunAndNode(t *testing.T) {
	reg := registry.NewMemoryRegistry()
	adapter := &fakeAdapter{failOn: map[string]bool{}}
	engine := NewDagEngine(reg, adapter)

	specInput := spec.ExecutableRunSpec{
		Run:   spec.RunMetadata{RunID: "run-terminal", SubmittedAt: time.Now().UTC(), FailurePolicy: spec.FailurePolicy{Mode: "fail-fast"}},
		Graph: spec.Graph{Nodes: []spec.Node{{NodeID: "a", Image: "busybox:1.36"}}},
	}
	finishedAt := time.Now().UTC()
	record := spec.RunRecord{
		RunID: specInput.Run.RunID, Status: spec.RunStatusSucceeded, AcceptedAt: time.Now().UTC(),
		TerminalStopCause: "finished", Spec: specInput,
	}
	nodes := []spec.NodeRecord{{
		RunID: record.RunID, NodeID: "a", Status: spec.NodeStatusSucceeded, FinishedAt: &finishedAt,
	}}
	if err := reg.CreateRun(context.Background(), record, nodes); err != nil {
		t.Fatalf("CreateRun() error = %v", err)
	}

	if err := engine.Cancel(context.Background(), record.RunID, "late_cancel_attempt"); err != nil {
		t.Fatalf("Cancel() error = %v", err)
	}

	got, err := reg.GetRun(context.Background(), record.RunID)
	if err != nil {
		t.Fatalf("GetRun() error = %v", err)
	}
	if got.Status != spec.RunStatusSucceeded {
		t.Fatalf("run status after Cancel on terminal run = %q, want %q (Cancel must not overwrite a terminal run)", got.Status, spec.RunStatusSucceeded)
	}

	gotNode, err := reg.GetNode(context.Background(), record.RunID, "a")
	if err != nil {
		t.Fatalf("GetNode() error = %v", err)
	}
	if gotNode.Status != spec.NodeStatusSucceeded {
		t.Fatalf("node status after Cancel on terminal run = %q, want %q", gotNode.Status, spec.NodeStatusSucceeded)
	}

	events, err := reg.ListEvents(context.Background(), record.RunID, 0)
	if err != nil {
		t.Fatalf("ListEvents() error = %v", err)
	}
	for _, ev := range events {
		if ev.Type == "node.canceled" {
			t.Fatalf("unexpected node.canceled event for an already-terminal node: %+v", ev)
		}
	}
}

// TestDagEngineNodeLevelRetryLoop exercises the failNode -> errNodeRetry ->
// new-attempt loop (RunE's for-loop in executor.go), which no existing test
// ran end-to-end: no test previously set RetryPolicy.MaxAttempts > 0.
// MaxAttempts is a total-attempt cap, not a retry count
// (docs/JUMI_EXECUTABLE_RUN_SPEC_DRAFT.ko.md 9.3: "maxAttempts = 1은 실행
// 1회, 재시도 없음이다"), so MaxAttempts=2 is required to exercise exactly
// one retry. The backend fails every WaitNode call, so the node must make
// exactly two attempts (the original plus one retry) before finally
// failing.
func TestDagEngineNodeLevelRetryLoop(t *testing.T) {
	reg := registry.NewMemoryRegistry()
	adapter := &fakeAdapter{failOn: map[string]bool{"a": true}}
	engine := NewDagEngine(reg, adapter)

	specInput := spec.ExecutableRunSpec{
		Run: spec.RunMetadata{RunID: "run-retry", SubmittedAt: time.Now().UTC(), FailurePolicy: spec.FailurePolicy{Mode: "fail-fast"}},
		Graph: spec.Graph{Nodes: []spec.Node{{
			NodeID: "a", Image: "busybox:1.36",
			RetryPolicy: spec.RetryPolicy{MaxAttempts: 2},
		}}},
	}
	record := spec.RunRecord{RunID: specInput.Run.RunID, Status: spec.RunStatusAccepted, AcceptedAt: time.Now().UTC(), Spec: specInput}
	nodes := []spec.NodeRecord{{RunID: record.RunID, NodeID: "a", Status: spec.NodeStatusPending}}
	if err := reg.CreateRun(context.Background(), record, nodes); err != nil {
		t.Fatalf("CreateRun() error = %v", err)
	}
	if err := engine.Admit(context.Background(), record); err != nil {
		t.Fatalf("Admit() error = %v", err)
	}

	waitForRunStatus(t, reg, record.RunID, spec.RunStatusFailed)

	gotNode, err := reg.GetNode(context.Background(), record.RunID, "a")
	if err != nil {
		t.Fatalf("GetNode() error = %v", err)
	}
	if gotNode.AttemptCount != 2 {
		t.Fatalf("attemptCount = %d, want 2 (1 original + 1 retry from MaxAttempts=2)", gotNode.AttemptCount)
	}
	if gotNode.Status != spec.NodeStatusFailed {
		t.Fatalf("final node status = %q, want %q", gotNode.Status, spec.NodeStatusFailed)
	}

	assertEventTypePresent(t, reg, record.RunID, "node.attempt_failed")
	assertEventTypePresent(t, reg, record.RunID, "node.failed")
}

// orphanedAfterRunningRegistry wraps a real Registry and, once armed,
// makes every subsequent UpdateNode/UpdateRun/GetRun call for a specific
// run return registry.ErrRunNotFound - simulating the parent run record
// having been deleted out from under a still-executing node, e.g. by a
// concurrent pipeline-delete operation. It arms itself automatically the
// first time the target node reaches spec.NodeStatusRunning, so the
// "deletion" lands mid-flight rather than before execution starts.
type orphanedAfterRunningRegistry struct {
	registry.Registry
	runID, nodeID string

	mu    sync.Mutex
	armed bool
}

func (o *orphanedAfterRunningRegistry) shouldFail() bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.armed
}

func (o *orphanedAfterRunningRegistry) arm() {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.armed = true
}

func (o *orphanedAfterRunningRegistry) UpdateNode(ctx context.Context, runID, nodeID string, update func(*spec.NodeRecord) error) error {
	if runID == o.runID && nodeID == o.nodeID {
		// Let the real store apply the update first so we can detect the
		// Running transition, then arm - the *next* call (waitAndFinalize's
		// terminal update) is what must observe the "parent deleted" error.
		err := o.Registry.UpdateNode(ctx, runID, nodeID, update)
		if err != nil {
			return err
		}
		rec, getErr := o.GetNode(ctx, runID, nodeID)
		if getErr == nil && rec.Status == spec.NodeStatusRunning {
			o.arm()
			return nil
		}
		if o.shouldFail() {
			return registry.ErrRunNotFound
		}
		return nil
	}
	return o.Registry.UpdateNode(ctx, runID, nodeID, update)
}

func (o *orphanedAfterRunningRegistry) GetRun(ctx context.Context, runID string) (spec.RunRecord, error) {
	if runID == o.runID && o.shouldFail() {
		return spec.RunRecord{}, registry.ErrRunNotFound
	}
	return o.Registry.GetRun(ctx, runID)
}

// TestDagEngineSurvivesParentDeletedWhileNodeRunning simulates the parent
// run record disappearing (e.g. a concurrent delete) while a node is still
// executing: once the node reaches Running, every further registry call
// for it starts returning ErrRunNotFound. executor.go discards most
// UpdateNode errors with "_ =", so this proves that discarding doesn't
// leave the executing goroutine hung or the engine's active-run bookkeeping
// leaked - it must still terminate within a bound and clean up
// getActiveRun(runID).
func TestDagEngineSurvivesParentDeletedWhileNodeRunning(t *testing.T) {
	base := registry.NewMemoryRegistry()
	reg := &orphanedAfterRunningRegistry{Registry: base, runID: "run-orphan", nodeID: "a"}
	adapter := &fakeAdapter{
		failOn: map[string]bool{},
		waitCh: map[string]chan struct{}{"a": make(chan struct{})},
	}
	engine := NewDagEngine(reg, adapter)

	specInput := spec.ExecutableRunSpec{
		Run:   spec.RunMetadata{RunID: "run-orphan", SubmittedAt: time.Now().UTC(), FailurePolicy: spec.FailurePolicy{Mode: "fail-fast"}},
		Graph: spec.Graph{Nodes: []spec.Node{{NodeID: "a", Image: "busybox:1.36"}}},
	}
	record := spec.RunRecord{RunID: specInput.Run.RunID, Status: spec.RunStatusAccepted, AcceptedAt: time.Now().UTC(), Spec: specInput}
	nodes := []spec.NodeRecord{{RunID: record.RunID, NodeID: "a", Status: spec.NodeStatusPending}}
	if err := base.CreateRun(context.Background(), record, nodes); err != nil {
		t.Fatalf("CreateRun() error = %v", err)
	}
	if err := engine.Admit(context.Background(), record); err != nil {
		t.Fatalf("Admit() error = %v", err)
	}

	// Wait until the node is Running (which arms the fault injector) before
	// letting the backend "finish" the job.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && !reg.shouldFail() {
		time.Sleep(10 * time.Millisecond)
	}
	if !reg.shouldFail() {
		t.Fatal("node never reached Running / fault injector never armed")
	}

	close(adapter.waitCh["a"]) // let WaitNode return, forcing waitAndFinalize's registry calls to hit ErrRunNotFound

	deadline = time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if engine.getActiveRun(record.RunID) == nil {
			return // clean exit: no leaked active-run bookkeeping, no hang
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("executor did not clean up active-run tracking within the deadline after the parent run disappeared - goroutine likely hung")
}
