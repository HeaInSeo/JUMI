package spec

import (
	"strings"
	"testing"
)

func minimalSpec(runID string, nodes []Node) ExecutableRunSpec {
	return ExecutableRunSpec{
		Run:   RunMetadata{RunID: runID},
		Graph: Graph{Nodes: nodes},
	}
}

func TestValidateExecutableRunSpec_HappyPath(t *testing.T) {
	tests := []struct {
		name string
		spec ExecutableRunSpec
	}{
		{
			name: "single node no edges",
			spec: minimalSpec("run-1", []Node{{NodeID: "a", Image: "busybox:1.36"}}),
		},
		{
			name: "two nodes with edge",
			spec: ExecutableRunSpec{
				Run: RunMetadata{RunID: "run-2"},
				Graph: Graph{
					Nodes: []Node{
						{NodeID: "a", Image: "img:1"},
						{NodeID: "b", Image: "img:2"},
					},
					Edges: [][]string{{"a", "b"}},
				},
			},
		},
		{
			name: "node with zero retry and timeout policy",
			spec: minimalSpec("run-3", []Node{{NodeID: "a", Image: "img:1", RetryPolicy: RetryPolicy{MaxAttempts: 0}, TimeoutPolicy: TimeoutPolicy{Seconds: 0}}}),
		},
		{
			name: "node with positive retry and timeout",
			spec: minimalSpec("run-4", []Node{{NodeID: "a", Image: "img:1", RetryPolicy: RetryPolicy{MaxAttempts: 3}, TimeoutPolicy: TimeoutPolicy{Seconds: 600}}}),
		},
		{
			name: "node with full artifact binding",
			spec: ExecutableRunSpec{
				Run: RunMetadata{RunID: "run-5"},
				Graph: Graph{
					Nodes: []Node{
						{NodeID: "producer", Image: "img:1"},
						{
							NodeID: "consumer",
							Image:  "img:2",
							ArtifactBindings: []ArtifactBinding{
								{
									BindingName:        "my-binding",
									ProducerNodeID:     "producer",
									ProducerOutputName: "output",
								},
							},
						},
					},
				},
			},
		},
		{
			name: "diamond graph",
			spec: ExecutableRunSpec{
				Run: RunMetadata{RunID: "run-6"},
				Graph: Graph{
					Nodes: []Node{
						{NodeID: "a", Image: "img:1"},
						{NodeID: "b", Image: "img:2"},
						{NodeID: "c", Image: "img:3"},
						{NodeID: "d", Image: "img:4"},
					},
					Edges: [][]string{{"a", "b"}, {"a", "c"}, {"b", "d"}, {"c", "d"}},
				},
			},
		},
		{
			name: "failure policy already set",
			spec: ExecutableRunSpec{
				Run:   RunMetadata{RunID: "run-7", FailurePolicy: FailurePolicy{Mode: "continue"}},
				Graph: Graph{Nodes: []Node{{NodeID: "a", Image: "img:1"}}},
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := ValidateExecutableRunSpec(tc.spec); err != nil {
				t.Fatalf("ValidateExecutableRunSpec() unexpected error: %v", err)
			}
		})
	}
}

func TestValidateExecutableRunSpec_MissingRunID(t *testing.T) {
	s := minimalSpec("", []Node{{NodeID: "a", Image: "img:1"}})
	err := ValidateExecutableRunSpec(s)
	if err == nil {
		t.Fatal("expected error for missing runId")
	}
	if !strings.Contains(err.Error(), "runId") {
		t.Fatalf("expected runId in error, got: %v", err)
	}
}

func TestValidateExecutableRunSpec_EmptyNodes(t *testing.T) {
	s := minimalSpec("run-1", nil)
	err := ValidateExecutableRunSpec(s)
	if err == nil {
		t.Fatal("expected error for empty nodes")
	}
	if !strings.Contains(err.Error(), "nodes") {
		t.Fatalf("expected nodes in error, got: %v", err)
	}
}

func TestValidateExecutableRunSpec_MissingNodeID(t *testing.T) {
	s := minimalSpec("run-1", []Node{{NodeID: "", Image: "img:1"}})
	err := ValidateExecutableRunSpec(s)
	if err == nil {
		t.Fatal("expected error for missing nodeId")
	}
	if !strings.Contains(err.Error(), "nodeId") {
		t.Fatalf("expected nodeId in error, got: %v", err)
	}
}

func TestValidateExecutableRunSpec_DuplicateNodeID(t *testing.T) {
	s := minimalSpec("run-1", []Node{
		{NodeID: "dup", Image: "img:1"},
		{NodeID: "dup", Image: "img:2"},
	})
	err := ValidateExecutableRunSpec(s)
	if err == nil {
		t.Fatal("expected error for duplicate nodeId")
	}
	if !strings.Contains(err.Error(), "dup") {
		t.Fatalf("expected nodeId in error, got: %v", err)
	}
}

func TestValidateExecutableRunSpec_MissingImage(t *testing.T) {
	s := minimalSpec("run-1", []Node{{NodeID: "a", Image: ""}})
	err := ValidateExecutableRunSpec(s)
	if err == nil {
		t.Fatal("expected error for missing image")
	}
	if !strings.Contains(err.Error(), "image") {
		t.Fatalf("expected image in error, got: %v", err)
	}
}

func TestValidateExecutableRunSpec_NegativeRetryMaxAttempts(t *testing.T) {
	s := minimalSpec("run-1", []Node{{NodeID: "a", Image: "img:1", RetryPolicy: RetryPolicy{MaxAttempts: -1}}})
	err := ValidateExecutableRunSpec(s)
	if err == nil {
		t.Fatal("expected error for negative maxAttempts")
	}
	if !strings.Contains(err.Error(), "maxAttempts") {
		t.Fatalf("expected maxAttempts in error, got: %v", err)
	}
}

func TestValidateExecutableRunSpec_NegativeTimeoutSeconds(t *testing.T) {
	s := minimalSpec("run-1", []Node{{NodeID: "a", Image: "img:1", TimeoutPolicy: TimeoutPolicy{Seconds: -1}}})
	err := ValidateExecutableRunSpec(s)
	if err == nil {
		t.Fatal("expected error for negative timeout seconds")
	}
	if !strings.Contains(err.Error(), "seconds") {
		t.Fatalf("expected seconds in error, got: %v", err)
	}
}

func TestValidateExecutableRunSpec_ArtifactBindingMissingBindingName(t *testing.T) {
	s := ExecutableRunSpec{
		Run: RunMetadata{RunID: "run-1"},
		Graph: Graph{
			Nodes: []Node{{
				NodeID: "a",
				Image:  "img:1",
				ArtifactBindings: []ArtifactBinding{
					{BindingName: "", ProducerNodeID: "x", ProducerOutputName: "out"},
				},
			}},
		},
	}
	err := ValidateExecutableRunSpec(s)
	if err == nil {
		t.Fatal("expected error for missing bindingName")
	}
	if !strings.Contains(err.Error(), "bindingName") {
		t.Fatalf("expected bindingName in error, got: %v", err)
	}
}

func TestValidateExecutableRunSpec_ArtifactBindingMissingProducerNodeID(t *testing.T) {
	s := ExecutableRunSpec{
		Run: RunMetadata{RunID: "run-1"},
		Graph: Graph{
			Nodes: []Node{{
				NodeID: "a",
				Image:  "img:1",
				ArtifactBindings: []ArtifactBinding{
					{BindingName: "b", ProducerNodeID: "", ProducerOutputName: "out"},
				},
			}},
		},
	}
	err := ValidateExecutableRunSpec(s)
	if err == nil {
		t.Fatal("expected error for missing producerNodeId")
	}
	if !strings.Contains(err.Error(), "producerNodeId") {
		t.Fatalf("expected producerNodeId in error, got: %v", err)
	}
}

func TestValidateExecutableRunSpec_ArtifactBindingMissingProducerOutputName(t *testing.T) {
	s := ExecutableRunSpec{
		Run: RunMetadata{RunID: "run-1"},
		Graph: Graph{
			Nodes: []Node{{
				NodeID: "a",
				Image:  "img:1",
				ArtifactBindings: []ArtifactBinding{
					{BindingName: "b", ProducerNodeID: "prod", ProducerOutputName: ""},
				},
			}},
		},
	}
	err := ValidateExecutableRunSpec(s)
	if err == nil {
		t.Fatal("expected error for missing producerOutputName")
	}
	if !strings.Contains(err.Error(), "producerOutputName") {
		t.Fatalf("expected producerOutputName in error, got: %v", err)
	}
}

func TestValidateExecutableRunSpec_SelfLoopEdge(t *testing.T) {
	s := ExecutableRunSpec{
		Run: RunMetadata{RunID: "run-1"},
		Graph: Graph{
			Nodes: []Node{{NodeID: "a", Image: "img:1"}},
			Edges: [][]string{{"a", "a"}},
		},
	}
	err := ValidateExecutableRunSpec(s)
	if err == nil {
		t.Fatal("expected error for self-loop edge")
	}
	if !strings.Contains(err.Error(), "self-loop") {
		t.Fatalf("expected self-loop in error, got: %v", err)
	}
}

func TestValidateExecutableRunSpec_EdgeSourceNonExistent(t *testing.T) {
	s := ExecutableRunSpec{
		Run: RunMetadata{RunID: "run-1"},
		Graph: Graph{
			Nodes: []Node{{NodeID: "a", Image: "img:1"}},
			Edges: [][]string{{"nonexistent", "a"}},
		},
	}
	err := ValidateExecutableRunSpec(s)
	if err == nil {
		t.Fatal("expected error for edge source not existing")
	}
	if !strings.Contains(err.Error(), "source node") {
		t.Fatalf("expected source node in error, got: %v", err)
	}
}

func TestValidateExecutableRunSpec_EdgeTargetNonExistent(t *testing.T) {
	s := ExecutableRunSpec{
		Run: RunMetadata{RunID: "run-1"},
		Graph: Graph{
			Nodes: []Node{{NodeID: "a", Image: "img:1"}},
			Edges: [][]string{{"a", "nonexistent"}},
		},
	}
	err := ValidateExecutableRunSpec(s)
	if err == nil {
		t.Fatal("expected error for edge target not existing")
	}
	if !strings.Contains(err.Error(), "target node") {
		t.Fatalf("expected target node in error, got: %v", err)
	}
}

func TestValidateExecutableRunSpec_CycleDetection(t *testing.T) {
	tests := []struct {
		name  string
		nodes []Node
		edges [][]string
	}{
		{
			name: "two-node cycle",
			nodes: []Node{
				{NodeID: "a", Image: "img:1"},
				{NodeID: "b", Image: "img:2"},
			},
			edges: [][]string{{"a", "b"}, {"b", "a"}},
		},
		{
			name: "three-node cycle",
			nodes: []Node{
				{NodeID: "a", Image: "img:1"},
				{NodeID: "b", Image: "img:2"},
				{NodeID: "c", Image: "img:3"},
			},
			edges: [][]string{{"a", "b"}, {"b", "c"}, {"c", "a"}},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := ExecutableRunSpec{
				Run:   RunMetadata{RunID: "run-cycle"},
				Graph: Graph{Nodes: tc.nodes, Edges: tc.edges},
			}
			err := ValidateExecutableRunSpec(s)
			if err == nil {
				t.Fatal("expected error for cycle")
			}
			if !strings.Contains(err.Error(), "acyclic") {
				t.Fatalf("expected acyclic in error, got: %v", err)
			}
		})
	}
}

func TestValidateExecutableRunSpec_EdgeWrongLength(t *testing.T) {
	s := ExecutableRunSpec{
		Run: RunMetadata{RunID: "run-1"},
		Graph: Graph{
			Nodes: []Node{{NodeID: "a", Image: "img:1"}},
			Edges: [][]string{{"a"}},
		},
	}
	err := ValidateExecutableRunSpec(s)
	if err == nil {
		t.Fatal("expected error for edge with one endpoint")
	}
	if !strings.Contains(err.Error(), "two endpoints") {
		t.Fatalf("expected 'two endpoints' in error, got: %v", err)
	}
}

func TestApplyDefaults_HappyPath(t *testing.T) {
	defaults := Defaults{
		ExecutionClass:  "standard",
		ResourceProfile: ResourceProfile{CPU: "500m", Memory: "256Mi"},
		TimeoutPolicy:   TimeoutPolicy{Seconds: 300},
		RetryPolicy:     RetryPolicy{MaxAttempts: 2},
		CleanupPolicy:   CleanupPolicy{TTLSecondsAfterFinished: 600},
		Placement:       &PlacementHints{RequiredNodeName: "worker-1"},
	}

	graph := &Graph{
		Nodes: []Node{
			{NodeID: "a", Image: "img:1"},
		},
	}
	ApplyDefaults(graph, defaults)

	n := graph.Nodes[0]
	if n.ExecutionClass != "standard" {
		t.Fatalf("ExecutionClass = %q, want standard", n.ExecutionClass)
	}
	if n.ResourceProfile.CPU != "500m" {
		t.Fatalf("ResourceProfile.CPU = %q, want 500m", n.ResourceProfile.CPU)
	}
	if n.TimeoutPolicy.Seconds != 300 {
		t.Fatalf("TimeoutPolicy.Seconds = %d, want 300", n.TimeoutPolicy.Seconds)
	}
	if n.RetryPolicy.MaxAttempts != 2 {
		t.Fatalf("RetryPolicy.MaxAttempts = %d, want 2", n.RetryPolicy.MaxAttempts)
	}
	if n.CleanupPolicy.TTLSecondsAfterFinished != 600 {
		t.Fatalf("CleanupPolicy.TTL = %d, want 600", n.CleanupPolicy.TTLSecondsAfterFinished)
	}
	if n.Placement == nil || n.Placement.RequiredNodeName != "worker-1" {
		t.Fatalf("Placement = %+v, want worker-1", n.Placement)
	}
}

func TestApplyDefaults_DoesNotOverrideExistingNodeFields(t *testing.T) {
	defaults := Defaults{
		ExecutionClass:  "standard",
		ResourceProfile: ResourceProfile{CPU: "500m", Memory: "256Mi"},
		TimeoutPolicy:   TimeoutPolicy{Seconds: 300},
		RetryPolicy:     RetryPolicy{MaxAttempts: 2},
		CleanupPolicy:   CleanupPolicy{TTLSecondsAfterFinished: 600},
		Placement:       &PlacementHints{RequiredNodeName: "default-worker"},
	}

	graph := &Graph{
		Nodes: []Node{
			{
				NodeID:          "a",
				Image:           "img:1",
				ExecutionClass:  "gpu",
				ResourceProfile: ResourceProfile{CPU: "2", Memory: "4Gi"},
				TimeoutPolicy:   TimeoutPolicy{Seconds: 60},
				RetryPolicy:     RetryPolicy{MaxAttempts: 5},
				CleanupPolicy:   CleanupPolicy{TTLSecondsAfterFinished: 3600},
				Placement:       &PlacementHints{RequiredNodeName: "specific-node"},
			},
		},
	}
	ApplyDefaults(graph, defaults)

	n := graph.Nodes[0]
	if n.ExecutionClass != "gpu" {
		t.Fatalf("ExecutionClass overridden, got %q want gpu", n.ExecutionClass)
	}
	if n.ResourceProfile.CPU != "2" {
		t.Fatalf("ResourceProfile.CPU overridden, got %q want 2", n.ResourceProfile.CPU)
	}
	if n.TimeoutPolicy.Seconds != 60 {
		t.Fatalf("TimeoutPolicy.Seconds overridden, got %d want 60", n.TimeoutPolicy.Seconds)
	}
	if n.RetryPolicy.MaxAttempts != 5 {
		t.Fatalf("RetryPolicy.MaxAttempts overridden, got %d want 5", n.RetryPolicy.MaxAttempts)
	}
	if n.CleanupPolicy.TTLSecondsAfterFinished != 3600 {
		t.Fatalf("CleanupPolicy.TTL overridden, got %d want 3600", n.CleanupPolicy.TTLSecondsAfterFinished)
	}
	if n.Placement.RequiredNodeName != "specific-node" {
		t.Fatalf("Placement overridden, got %q want specific-node", n.Placement.RequiredNodeName)
	}
}

func TestApplyDefaults_EmptyDefaultsDoesNothing(t *testing.T) {
	graph := &Graph{
		Nodes: []Node{
			{NodeID: "a", Image: "img:1"},
		},
	}
	before := graph.Nodes[0]
	ApplyDefaults(graph, Defaults{})
	after := graph.Nodes[0]

	if before.ExecutionClass != after.ExecutionClass {
		t.Fatal("empty defaults changed ExecutionClass")
	}
	if before.ResourceProfile != after.ResourceProfile {
		t.Fatal("empty defaults changed ResourceProfile")
	}
	if before.TimeoutPolicy != after.TimeoutPolicy {
		t.Fatal("empty defaults changed TimeoutPolicy")
	}
}

func TestApplyDefaults_PlacementCopiedByValue(t *testing.T) {
	placement := &PlacementHints{RequiredNodeName: "shared-node"}
	defaults := Defaults{Placement: placement}

	graph := &Graph{
		Nodes: []Node{
			{NodeID: "a", Image: "img:1"},
			{NodeID: "b", Image: "img:2"},
		},
	}
	ApplyDefaults(graph, defaults)

	// Mutating the original placement should not affect nodes.
	placement.RequiredNodeName = "mutated"
	if graph.Nodes[0].Placement.RequiredNodeName == "mutated" {
		t.Fatal("placement was not deep-copied; original mutation affected node")
	}
}

func TestApplyDefaults_MultipleNodes(t *testing.T) {
	defaults := Defaults{ExecutionClass: "batch"}
	graph := &Graph{
		Nodes: []Node{
			{NodeID: "a", Image: "img:1"},
			{NodeID: "b", Image: "img:2", ExecutionClass: "gpu"},
			{NodeID: "c", Image: "img:3"},
		},
	}
	ApplyDefaults(graph, defaults)

	if graph.Nodes[0].ExecutionClass != "batch" {
		t.Fatalf("node a ExecutionClass = %q, want batch", graph.Nodes[0].ExecutionClass)
	}
	if graph.Nodes[1].ExecutionClass != "gpu" {
		t.Fatalf("node b ExecutionClass overridden to %q, want gpu", graph.Nodes[1].ExecutionClass)
	}
	if graph.Nodes[2].ExecutionClass != "batch" {
		t.Fatalf("node c ExecutionClass = %q, want batch", graph.Nodes[2].ExecutionClass)
	}
}
