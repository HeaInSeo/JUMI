package executor

import (
	"testing"

	"github.com/HeaInSeo/JUMI/pkg/handoff"
	"github.com/HeaInSeo/JUMI/pkg/provenance"
	"github.com/HeaInSeo/JUMI/pkg/spec"
)

func TestSanitizeResolvedLocalPath(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "safe inputs child", input: "inputs/result", want: "inputs/result"},
		{name: "safe nested inputs child", input: "inputs/subdir/result", want: "inputs/subdir/result"},
		{name: "absolute rejected", input: "/work/inputs/result", want: ""},
		{name: "outside inputs rejected", input: "result", want: ""},
		{name: "escape rejected", input: "../etc/passwd", want: ""},
		{name: "inputs root rejected", input: "inputs", want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := sanitizeResolvedLocalPath(tt.input); got != tt.want {
				t.Fatalf("sanitizeResolvedLocalPath(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestInjectResolvedBindingEnvIgnoresUnsafeLocalPath(t *testing.T) {
	node := &spec.Node{}
	binding := spec.ArtifactBinding{
		BindingName:        "dataset",
		ChildInputName:     "dataset",
		ProducerOutputName: "dataset",
	}
	resolved := handoff.ResolveBindingResponse{
		ResolutionStatus: "RESOLVED",
		Decision:         "local_reuse",
		MaterializationPlan: handoff.MaterializationPlan{
			Mode:           "local_reuse",
			ExpectedDigest: "sha256:abc",
			LocalPath:      "/etc/passwd",
		},
	}

	injectResolvedBindingEnv(node, binding, resolved)

	if _, ok := node.Env["JUMI_INPUT_DATASET_LOCAL_PATH"]; ok {
		t.Fatal("expected unsafe local path to be omitted from env")
	}
}

func TestInjectResolvedBindingEnvAddsExpectedSizeBytes(t *testing.T) {
	node := &spec.Node{}
	binding := spec.ArtifactBinding{
		BindingName:        "dataset",
		ChildInputName:     "dataset",
		ProducerOutputName: "dataset",
	}
	resolved := handoff.ResolveBindingResponse{
		ResolutionStatus: "RESOLVED",
		Decision:         "remote_fetch",
		MaterializationPlan: handoff.MaterializationPlan{
			Mode:           "remote_fetch",
			ExpectedDigest: "sha256:abc",
			ExpectedSize:   17,
			LocalPath:      "inputs/result",
		},
	}

	injectResolvedBindingEnv(node, binding, resolved)

	if got := node.Env["JUMI_INPUT_DATASET_EXPECTED_SIZE_BYTES"]; got != "17" {
		t.Fatalf("JUMI_INPUT_DATASET_EXPECTED_SIZE_BYTES = %q, want 17", got)
	}
	if got := node.Env["JUMI_INPUT_DATASET_LOCAL_PATH"]; got != "inputs/result" {
		t.Fatalf("JUMI_INPUT_DATASET_LOCAL_PATH = %q, want inputs/result", got)
	}
}

func TestToHandoffLocationsBackfillsNodeName(t *testing.T) {
	locations := toHandoffLocations([]provenance.ArtifactLocation{{
		NodeLocal: &provenance.NodeLocalLocation{
			Path: "/var/lib/jumi-artifacts/cas/sha256/abc",
		},
	}}, "worker-2")

	if len(locations) != 1 || locations[0].NodeLocal == nil {
		t.Fatalf("locations = %#v, want one nodeLocal location", locations)
	}
	if locations[0].NodeLocal.NodeName != "worker-2" {
		t.Fatalf("nodeLocal.nodeName = %q, want worker-2", locations[0].NodeLocal.NodeName)
	}
}
