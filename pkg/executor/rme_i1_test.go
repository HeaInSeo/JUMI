package executor

import (
	"context"
	"testing"
	"time"

	"github.com/HeaInSeo/JUMI/pkg/backend"
	"github.com/HeaInSeo/JUMI/pkg/registry"
	"github.com/HeaInSeo/JUMI/pkg/spec"
)

// RME-I1 Correction 1 (integration): a replay-safe pre-fence realization failure that
// re-realizes MUST durably record the re-attempt basis on the just-failed Attempt, so
// that after crash -> restart -> reconcile the re-realization is provable from durable
// Attempt truth (see the ClassifyReconcile unit test) rather than the in-process
// errNodeRetry signal. The budget-exhausted terminal failure must NOT record a basis.
func TestRMEI1_RealizationReattemptBasisIsDurable(t *testing.T) {
	reg := registry.NewMemoryRegistry()
	withRealizationCeiling(t, 2)
	adapter := &fakeAdapter{failOn: map[string]bool{}, failPrepareOn: map[string]bool{"a": true}}
	engine := NewDagEngine(reg, adapter)

	specInput := spec.ExecutableRunSpec{
		Run: spec.RunMetadata{RunID: "run-rmei1-basis", SubmittedAt: time.Now().UTC(), FailurePolicy: spec.FailurePolicy{Mode: "fail-fast"}},
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

	attempts, err := reg.ListAttempts(context.Background(), record.RunID, "a")
	if err != nil {
		t.Fatalf("ListAttempts() error = %v", err)
	}
	if len(attempts) != 2 {
		t.Fatalf("attempts = %d, want 2 (realization ceiling=2)", len(attempts))
	}
	byID := map[string]spec.AttemptRecord{}
	for _, at := range attempts {
		byID[at.AttemptID] = at
	}
	first := spec.DeterministicAttemptID(record.RunID, "a", 1)
	final := spec.DeterministicAttemptID(record.RunID, "a", 2)

	a1, ok := byID[first]
	if !ok {
		t.Fatalf("missing first realization attempt %q", first)
	}
	if a1.RealizationReattemptableAt == nil {
		t.Fatalf("first realization attempt must durably record RealizationReattemptableAt; got %+v", a1)
	}
	if a1.SubmissionWindowOpenedAt != nil {
		t.Fatalf("realization failure must be pre-fence (SubmissionWindowOpenedAt nil); got %+v", a1)
	}
	if !a1.Status.IsTerminal() {
		t.Fatalf("first realization attempt must be terminal Errored; got status %q", a1.Status)
	}

	aF, ok := byID[final]
	if !ok {
		t.Fatalf("missing final realization attempt %q", final)
	}
	if aF.RealizationReattemptableAt != nil {
		t.Fatalf("final (budget-exhausted) realization attempt must NOT record a re-attempt basis; got %+v", aF)
	}

	// F3-B3 preserved: pre-fence realization failures consume zero user-code opportunities.
	gotNode, err := reg.GetNode(context.Background(), record.RunID, "a")
	if err != nil {
		t.Fatalf("GetNode() error = %v", err)
	}
	if gotNode.AttemptCount != 0 {
		t.Fatalf("attemptCount = %d, want 0 (realization-only failures never consume MaxAttempts)", gotNode.AttemptCount)
	}
}

// RME-I1 Correction 2 (integration): when a root node execution failure triggers a
// fast-fail control decision that cancels downstream work, the downstream node's
// durable terminal record MUST preserve the causal root (root execution failure ->
// fast-fail decision -> downstream cancellation) instead of a flat terminal that
// loses it. The root node keeps its OWN terminal cause and is never annotated with a
// causal link to another node.
func TestRMEI1_FastFailDownstreamRecordsCausalRoot(t *testing.T) {
	reg := registry.NewMemoryRegistry()
	adapter := &fakeAdapter{
		failOn:      map[string]bool{},
		waitResults: map[string]backend.ExecutionResult{"b1": {Succeeded: false, TerminalStopCause: "failed", TerminalFailureReason: "user_code_failed"}},
		waitCh:      map[string]chan struct{}{"b2": make(chan struct{})},
	}
	engine := NewDagEngine(reg, adapter)
	specInput := spec.ExecutableRunSpec{
		Run: spec.RunMetadata{RunID: "run-rmei1-cause", SubmittedAt: time.Now().UTC(), FailurePolicy: spec.FailurePolicy{Mode: "fail-fast"}},
		Graph: spec.Graph{
			Nodes: []spec.Node{
				{NodeID: "a", Image: "busybox:1.36"},
				{NodeID: "b1", Image: "busybox:1.36"},
				{NodeID: "b2", Image: "busybox:1.36"},
				{NodeID: "c", Image: "busybox:1.36"},
			},
			Edges: [][]string{{"a", "b1"}, {"a", "b2"}, {"b1", "c"}, {"b2", "c"}},
		},
	}
	record := spec.RunRecord{RunID: specInput.Run.RunID, Status: spec.RunStatusAccepted, AcceptedAt: time.Now().UTC(), Spec: specInput}
	nodes := []spec.NodeRecord{
		{RunID: record.RunID, NodeID: "a", Status: spec.NodeStatusPending},
		{RunID: record.RunID, NodeID: "b1", Status: spec.NodeStatusPending},
		{RunID: record.RunID, NodeID: "b2", Status: spec.NodeStatusPending},
		{RunID: record.RunID, NodeID: "c", Status: spec.NodeStatusPending},
	}
	if err := reg.CreateRun(context.Background(), record, nodes); err != nil {
		t.Fatalf("CreateRun() error = %v", err)
	}
	if err := engine.Admit(context.Background(), record); err != nil {
		t.Fatalf("Admit() error = %v", err)
	}
	waitForRunStatus(t, reg, record.RunID, spec.RunStatusFailed)

	cNode, err := reg.GetNode(context.Background(), record.RunID, "c")
	if err != nil {
		t.Fatalf("GetNode(c) error = %v", err)
	}
	if cNode.Status != spec.NodeStatusSkipped {
		t.Fatalf("downstream node c status = %q, want Skipped", cNode.Status)
	}
	if cNode.CausedByNodeID != "b1" {
		t.Fatalf("downstream skip c CausedByNodeID = %q, want b1 (fast-fail causal root preserved)", cNode.CausedByNodeID)
	}
	// The downstream terminal is a distinct dependency-skip, not the node's own failure.
	if cNode.TerminalFailureReason != "dependency_failed" {
		t.Fatalf("downstream skip c reason = %q, want dependency_failed", cNode.TerminalFailureReason)
	}

	// The root node keeps its OWN terminal cause and is not annotated with a causal
	// link to another node (it is the cause, not the caused).
	b1Node, err := reg.GetNode(context.Background(), record.RunID, "b1")
	if err != nil {
		t.Fatalf("GetNode(b1) error = %v", err)
	}
	if b1Node.Status != spec.NodeStatusFailed {
		t.Fatalf("root node b1 status = %q, want Failed", b1Node.Status)
	}
	if b1Node.CausedByNodeID != "" {
		t.Fatalf("root node b1 must not carry a causal link to another node; got %q", b1Node.CausedByNodeID)
	}
}
