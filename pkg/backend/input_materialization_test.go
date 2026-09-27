package backend

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/HeaInSeo/JUMI/pkg/spec"
)

// Issue #3 (D1/D2): nan is the only component that materializes inputs and exports the
// real JUMI_INPUT_<B>_LOCAL_PATH. These tests pin that an input needing materialization
// is never launched without nan, and that a launched node never carries an input
// materialization claim nothing will honor.

// consumerEnv is the input env the executor injects for one resolved binding "B".
func consumerEnv(mode string) map[string]string {
	requires := "true"
	if mode == "" || mode == "none" {
		requires = "false"
	}
	return map[string]string{
		"JUMI_ATTEMPT_ID":                       "att-1",
		"JUMI_INPUT_B_URI":                      "http://artifact.local/b",
		"JUMI_INPUT_B_MATERIALIZATION_MODE":     mode,
		"JUMI_INPUT_B_EXPECTED_DIGEST":          "sha256:abc",
		"JUMI_INPUT_B_LOCAL_PATH":               "inputs/b",
		"JUMI_INPUT_B_REQUIRES_MATERIALIZATION": requires,
	}
}

func runtimeHelperMeta() map[string]string {
	return map[string]string{outputManifestModeMetadataKey: outputManifestModeRuntimeHelper}
}

func isNanWrapped(command []string) bool {
	return len(command) >= 3 && command[0] == "/bin/sh" && command[1] == "-ceu" && strings.Contains(command[2], " run --contract ")
}

// D1: an input-only consumer (no outputs) whose input requires materialization is
// wrapped by nan, and its contract carries the input so nan knows what to materialize.
func TestD1_InputOnlyConsumerIsWrappedByNan(t *testing.T) {
	for _, mode := range []string{"remote_fetch", "local_reuse"} {
		t.Run(mode, func(t *testing.T) {
			node := spec.Node{NodeID: "consumer", Image: "img:1", Command: []string{"cat", "in"}, Env: consumerEnv(mode), Metadata: runtimeHelperMeta()}
			req := toAttemptRequest(spec.RunRecord{RunID: "run-1"}, node)
			if !isNanWrapped(req.Command) {
				t.Fatalf("input-only consumer (%s) not wrapped by nan: %v", mode, req.Command)
			}
			raw := req.Env["JUMI_NODE_CONTRACT_JSON"]
			if raw == "" {
				t.Fatal("wrapped consumer has no node contract")
			}
			var contract nodeContractFile
			if err := json.Unmarshal([]byte(raw), &contract); err != nil {
				t.Fatalf("contract JSON: %v", err)
			}
			if len(contract.Inputs) != 1 || contract.Inputs[0].Name != "b" || contract.Inputs[0].MaterializationMode != mode {
				t.Fatalf("contract inputs = %+v, want input b (%s)", contract.Inputs, mode)
			}
			if len(contract.Outputs) != 0 {
				t.Fatalf("contract outputs = %+v, want none", contract.Outputs)
			}
			// nan owns the real input path: the planned one must not reach the pod.
			if v, ok := req.Env["JUMI_INPUT_B_LOCAL_PATH"]; ok {
				t.Fatalf("planned JUMI_INPUT_B_LOCAL_PATH=%q leaked into a nan-wrapped pod", v)
			}
			// No manifest export is claimed for a node without outputs.
			if _, ok := req.Env["JUMI_OUTPUT_MANIFEST_ENABLED"]; ok {
				t.Fatal("manifest export enabled for a node with no outputs")
			}
		})
	}
}

// D1 negative: nothing to materialize and nothing to export → no wrap.
func TestD1_NoOutputsNoMaterializationNotWrapped(t *testing.T) {
	cases := map[string]map[string]string{
		"no inputs":         {"JUMI_ATTEMPT_ID": "att-1"},
		"input mode none":   consumerEnv("none"),
		"input mode absent": consumerEnv(""),
	}
	for name, env := range cases {
		t.Run(name, func(t *testing.T) {
			node := spec.Node{NodeID: "n", Image: "img:1", Command: []string{"true"}, Env: env, Metadata: runtimeHelperMeta()}
			req := toAttemptRequest(spec.RunRecord{RunID: "run-1"}, node)
			if isNanWrapped(req.Command) {
				t.Fatalf("wrapped without outputs or materialization: %v", req.Command)
			}
			if err := checkInputMaterializationRuntime(node); err != nil {
				t.Fatalf("unexpected fail-closed: %v", err)
			}
		})
	}
}

// D2: the invariant "an input materialization claim is launched ⟺ nan wraps the node",
// checked through PrepareNode over every combination of outputs × runtime-helper
// declaration × input materialization. The one combination that cannot honor it —
// materialization required, runtime not declared runtime-helper — must fail closed
// before submission instead of launching with a planned path nothing creates.
func TestD2_InputMaterializationClaimImpliesNanWrap(t *testing.T) {
	adapter := &SpawnerK8sAdapter{}
	for _, outputs := range [][]string{nil, {"report"}} {
		for _, helper := range []bool{false, true} {
			for _, mode := range []string{"none", "remote_fetch"} {
				name := strings.Join([]string{"outputs=" + strings.Join(outputs, ","), "helper=" + map[bool]string{true: "yes", false: "no"}[helper], "mode=" + mode}, "/")
				t.Run(name, func(t *testing.T) {
					node := spec.Node{NodeID: "n", Image: "img:1", Command: []string{"run"}, Outputs: outputs, Env: consumerEnv(mode)}
					if helper {
						node.Metadata = runtimeHelperMeta()
					}
					requires := mode != "none"

					prepared, err := adapter.PrepareNode(context.Background(), spec.RunRecord{RunID: "run-1"}, node)
					if requires && !helper {
						if !errors.Is(err, ErrInputMaterializationUnavailable) {
							t.Fatalf("PrepareNode err = %v, want ErrInputMaterializationUnavailable (fail closed)", err)
						}
						return
					}
					if err != nil {
						t.Fatalf("PrepareNode: %v", err)
					}
					req := prepared.(preparedRuntimeNode).req
					wrapped := isNanWrapped(req.Command)
					if requires && !wrapped {
						t.Fatalf("launched a materialization-requiring input without nan: %v", req.Command)
					}
					if wrapped {
						if _, ok := req.Env["JUMI_INPUT_B_LOCAL_PATH"]; ok {
							t.Fatal("nan-wrapped pod still carries the planned JUMI_INPUT_B_LOCAL_PATH")
						}
					}
					if !wrapped && strings.EqualFold(req.Env["JUMI_INPUT_B_REQUIRES_MATERIALIZATION"], "true") {
						t.Fatal("unwrapped pod claims JUMI_INPUT_B_REQUIRES_MATERIALIZATION=true")
					}
				})
			}
		}
	}
}

// D2: an explicit REQUIRES_MATERIALIZATION=true alone (no parsed contract mode) still
// counts as a materialization claim.
func TestD2_RequiresMaterializationFlagAloneFailsClosedWithoutNan(t *testing.T) {
	node := spec.Node{NodeID: "n", Image: "img:1", Env: map[string]string{"JUMI_INPUT_X_REQUIRES_MATERIALIZATION": "true"}}
	if err := checkInputMaterializationRuntime(node); !errors.Is(err, ErrInputMaterializationUnavailable) {
		t.Fatalf("err = %v, want ErrInputMaterializationUnavailable", err)
	}
}
