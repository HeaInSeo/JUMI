package spec

import (
	"fmt"
	"reflect"
	"strings"
)

const hostnameNodeSelectorKey = "kubernetes.io/hostname"

func ValidateExecutableRunSpec(spec ExecutableRunSpec) error {
	if spec.Run.RunID == "" {
		return fmt.Errorf("run.runId is required")
	}
	if len(spec.Graph.Nodes) == 0 {
		return fmt.Errorf("graph.nodes must not be empty")
	}
	if spec.Run.FailurePolicy.Mode == "" {
		spec.Run.FailurePolicy.Mode = "fail-fast"
	}

	nodes := make(map[string]struct{}, len(spec.Graph.Nodes))
	for _, node := range spec.Graph.Nodes {
		if node.NodeID == "" {
			return fmt.Errorf("graph.nodes[].nodeId is required")
		}
		if _, exists := nodes[node.NodeID]; exists {
			return fmt.Errorf("duplicate nodeId: %s", node.NodeID)
		}
		nodes[node.NodeID] = struct{}{}
		if node.Image == "" {
			return fmt.Errorf("node %s: image is required", node.NodeID)
		}
		if node.RetryPolicy.MaxAttempts < 0 {
			return fmt.Errorf("node %s: retryPolicy.maxAttempts must be >= 0", node.NodeID)
		}
		if node.TimeoutPolicy.Seconds < 0 {
			return fmt.Errorf("node %s: timeoutPolicy.seconds must be >= 0", node.NodeID)
		}
		if node.Placement != nil && node.Placement.RequiredNodeName != "" && node.Placement.NodeSelector != nil {
			if existing := strings.TrimSpace(node.Placement.NodeSelector[hostnameNodeSelectorKey]); existing != "" && existing != node.Placement.RequiredNodeName {
				return fmt.Errorf("node %s: requiredNodeName %q conflicts with nodeSelector[%q]=%q", node.NodeID, node.Placement.RequiredNodeName, hostnameNodeSelectorKey, existing)
			}
		}
		if node.Kueue != nil {
			for k := range node.Kueue.Labels {
				if !strings.HasPrefix(k, "user.jumi.io/") {
					return fmt.Errorf("node %s: kueue.labels[%q] must use user.jumi.io/ prefix", node.NodeID, k)
				}
			}
		}
		for _, binding := range node.ArtifactBindings {
			if binding.BindingName == "" {
				return fmt.Errorf("node %s: artifactBindings[].bindingName is required", node.NodeID)
			}
			if binding.ProducerNodeID == "" {
				return fmt.Errorf("node %s: artifactBinding %s producerNodeId is required", node.NodeID, binding.BindingName)
			}
			if binding.ProducerOutputName == "" {
				return fmt.Errorf("node %s: artifactBinding %s producerOutputName is required", node.NodeID, binding.BindingName)
			}
		}
	}

	adj := make(map[string][]string, len(spec.Graph.Nodes))
	for _, edge := range spec.Graph.Edges {
		if len(edge) != 2 {
			return fmt.Errorf("each edge must have exactly two endpoints")
		}
		from, to := edge[0], edge[1]
		if from == to {
			return fmt.Errorf("self-loop is not allowed: %s", from)
		}
		if _, ok := nodes[from]; !ok {
			return fmt.Errorf("edge source node does not exist: %s", from)
		}
		if _, ok := nodes[to]; !ok {
			return fmt.Errorf("edge target node does not exist: %s", to)
		}
		adj[from] = append(adj[from], to)
	}

	seen := make(map[string]bool, len(spec.Graph.Nodes))
	stack := make(map[string]bool, len(spec.Graph.Nodes))
	var visit func(string) error
	visit = func(node string) error {
		if stack[node] {
			return fmt.Errorf("graph must be acyclic")
		}
		if seen[node] {
			return nil
		}
		seen[node] = true
		stack[node] = true
		for _, next := range adj[node] {
			if err := visit(next); err != nil {
				return err
			}
		}
		stack[node] = false
		return nil
	}
	for node := range nodes {
		if err := visit(node); err != nil {
			return err
		}
	}
	return nil
}

// ApplyDefaults fills node-level fields from Defaults when the node field is
// the zero value. Mutates the graph nodes in-place.
func ApplyDefaults(graph *Graph, defaults Defaults) {
	for i := range graph.Nodes {
		n := &graph.Nodes[i]
		if n.ExecutionClass == "" && defaults.ExecutionClass != "" {
			n.ExecutionClass = defaults.ExecutionClass
		}
		if reflect.DeepEqual(n.ResourceProfile, ResourceProfile{}) && !reflect.DeepEqual(defaults.ResourceProfile, ResourceProfile{}) {
			n.ResourceProfile = defaults.ResourceProfile
		}
		if n.TimeoutPolicy.Seconds == 0 && defaults.TimeoutPolicy.Seconds != 0 {
			n.TimeoutPolicy = defaults.TimeoutPolicy
		}
		if n.RetryPolicy.MaxAttempts == 0 && reflect.DeepEqual(n.RetryPolicy, RetryPolicy{}) && !reflect.DeepEqual(defaults.RetryPolicy, RetryPolicy{}) {
			n.RetryPolicy = defaults.RetryPolicy
		}
		if reflect.DeepEqual(n.CleanupPolicy, CleanupPolicy{}) && !reflect.DeepEqual(defaults.CleanupPolicy, CleanupPolicy{}) {
			n.CleanupPolicy = defaults.CleanupPolicy
		}
		if n.Placement == nil && defaults.Placement != nil {
			p := *defaults.Placement
			n.Placement = &p
		}
	}
}
