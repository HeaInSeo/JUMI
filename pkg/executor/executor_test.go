package executor

import (
	"strings"
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

func TestInjectResolvedBindingEnvRemoteFetchContract(t *testing.T) {
	node := &spec.Node{}
	binding := spec.ArtifactBinding{
		BindingName:        "aligned-bam",
		ChildInputName:     "aligned.bam",
		ProducerOutputName: "bam",
	}
	resolved := handoff.ResolveBindingResponse{
		ResolutionStatus: "RESOLVED",
		Decision:         "remote_fetch",
		PlacementIntent:  handoff.PlacementIntent{Mode: placementModePreferredNode, NodeName: "worker-1"},
		MaterializationPlan: handoff.MaterializationPlan{
			Mode:           materializationModeRemoteFetch,
			URI:            "http://artifact.local/aligned.bam",
			ExpectedDigest: "sha256:abc",
			ExpectedSize:   120,
			LocalPath:      "inputs/aligned.bam",
		},
	}

	injectResolvedBindingEnv(node, binding, resolved)

	want := map[string]string{
		"JUMI_INPUT_ALIGNED_BAM_STATUS":                   "RESOLVED",
		"JUMI_INPUT_ALIGNED_BAM_DECISION":                 "remote_fetch",
		"JUMI_INPUT_ALIGNED_BAM_URI":                      "http://artifact.local/aligned.bam",
		"JUMI_INPUT_ALIGNED_BAM_SOURCE_NODE":              "worker-1",
		"JUMI_INPUT_ALIGNED_BAM_PLACEMENT_MODE":           placementModePreferredNode,
		"JUMI_INPUT_ALIGNED_BAM_MATERIALIZATION_MODE":     materializationModeRemoteFetch,
		"JUMI_INPUT_ALIGNED_BAM_EXPECTED_DIGEST":          "sha256:abc",
		"JUMI_INPUT_ALIGNED_BAM_EXPECTED_SIZE_BYTES":      "120",
		"JUMI_INPUT_ALIGNED_BAM_LOCAL_PATH":               "inputs/aligned.bam",
		"JUMI_INPUT_ALIGNED_BAM_REQUIRES_MATERIALIZATION": "true",
	}
	for key, value := range want {
		if got := node.Env[key]; got != value {
			t.Fatalf("%s = %q, want %q", key, got, value)
		}
	}
	if _, ok := node.Env["JUMI_INPUT_ALIGNED_BAM_NODE_LOCAL_PATH"]; ok {
		t.Fatal("remote_fetch env unexpectedly includes NODE_LOCAL_PATH")
	}
}

func TestInjectResolvedBindingEnvLocalReuseContract(t *testing.T) {
	node := &spec.Node{}
	binding := spec.ArtifactBinding{
		BindingName:        "result",
		ChildInputName:     "result",
		ProducerOutputName: "result",
	}
	resolved := handoff.ResolveBindingResponse{
		ResolutionStatus: "RESOLVED",
		Decision:         "local_reuse",
		PlacementIntent:  handoff.PlacementIntent{Mode: placementModeRequiredNode, NodeName: "worker-1"},
		MaterializationPlan: handoff.MaterializationPlan{
			Mode:           materializationModeLocalReuse,
			ExpectedDigest: "sha256:def",
			ExpectedSize:   42,
			LocalPath:      "inputs/result",
			SourceLocation: &handoff.ArtifactLocation{
				NodeLocal: &handoff.NodeLocalLocation{NodeName: "worker-1", Path: "/var/lib/jumi-artifacts/cas/sha256/def"},
			},
		},
	}

	injectResolvedBindingEnv(node, binding, resolved)

	want := map[string]string{
		"JUMI_INPUT_RESULT_STATUS":                   "RESOLVED",
		"JUMI_INPUT_RESULT_DECISION":                 "local_reuse",
		"JUMI_INPUT_RESULT_SOURCE_NODE":              "worker-1",
		"JUMI_INPUT_RESULT_PLACEMENT_MODE":           placementModeRequiredNode,
		"JUMI_INPUT_RESULT_MATERIALIZATION_MODE":     materializationModeLocalReuse,
		"JUMI_INPUT_RESULT_EXPECTED_DIGEST":          "sha256:def",
		"JUMI_INPUT_RESULT_EXPECTED_SIZE_BYTES":      "42",
		"JUMI_INPUT_RESULT_NODE_LOCAL_PATH":          "/var/lib/jumi-artifacts/cas/sha256/def",
		"JUMI_INPUT_RESULT_LOCAL_PATH":               "inputs/result",
		"JUMI_INPUT_RESULT_REQUIRES_MATERIALIZATION": "true",
	}
	for key, value := range want {
		if got := node.Env[key]; got != value {
			t.Fatalf("%s = %q, want %q", key, got, value)
		}
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

func TestValidateResolvedBindingEnvKeysRejectsCollisions(t *testing.T) {
	err := validateResolvedBindingEnvKeys([]spec.ArtifactBinding{
		{BindingName: "a-b", ProducerOutputName: "out-a"},
		{BindingName: "a_b", ProducerOutputName: "out-b"},
	})
	if err == nil {
		t.Fatal("expected env key collision error")
	}
}

func TestMaterializationBadFixtureMatrix(t *testing.T) {
	tests := []struct {
		name     string
		validate func() error
	}{
		{
			name: "placement required_node without nodeName",
			validate: func() error {
				return validateResolvedBindingContract(spec.ArtifactBinding{BindingName: "dataset"}, handoff.ResolveBindingResponse{
					ResolutionStatus: "RESOLVED",
					PlacementIntent:  handoff.PlacementIntent{Mode: placementModeRequiredNode},
					MaterializationPlan: handoff.MaterializationPlan{
						Mode: materializationModeNone,
					},
				})
			},
		},
		{
			name: "unsupported placement mode",
			validate: func() error {
				return validateResolvedBindingContract(spec.ArtifactBinding{BindingName: "dataset"}, handoff.ResolveBindingResponse{
					ResolutionStatus: "RESOLVED",
					PlacementIntent:  handoff.PlacementIntent{Mode: "same_rack", NodeName: "worker-1"},
					MaterializationPlan: handoff.MaterializationPlan{
						Mode: materializationModeNone,
					},
				})
			},
		},
		{
			name: "local_reuse without node-local source",
			validate: func() error {
				return validateResolvedBindingContract(spec.ArtifactBinding{BindingName: "dataset"}, handoff.ResolveBindingResponse{
					ResolutionStatus: "RESOLVED",
					MaterializationPlan: handoff.MaterializationPlan{
						Mode: materializationModeLocalReuse,
					},
				})
			},
		},
		{
			name: "remote_fetch without fetchable source",
			validate: func() error {
				return validateResolvedBindingContract(spec.ArtifactBinding{BindingName: "dataset"}, handoff.ResolveBindingResponse{
					ResolutionStatus: "RESOLVED",
					MaterializationPlan: handoff.MaterializationPlan{
						Mode: materializationModeRemoteFetch,
					},
				})
			},
		},
		{
			name: "unsafe materialization localPath",
			validate: func() error {
				return validateResolvedBindingContract(spec.ArtifactBinding{BindingName: "dataset"}, handoff.ResolveBindingResponse{
					ResolutionStatus: "RESOLVED",
					MaterializationPlan: handoff.MaterializationPlan{
						Mode:      materializationModeRemoteFetch,
						URI:       "http://artifact.local/dataset",
						LocalPath: "../escape",
					},
				})
			},
		},
		{
			name: "input env key collision",
			validate: func() error {
				return validateResolvedBindingEnvKeys([]spec.ArtifactBinding{
					{BindingName: "a-b", ProducerOutputName: "out-a"},
					{BindingName: "a_b", ProducerOutputName: "out-b"},
				})
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.validate(); err == nil {
				t.Fatal("validate() error = nil, want bad fixture rejection")
			} else if !strings.Contains(err.Error(), "input_materialization_contract_invalid") && !strings.Contains(err.Error(), "input env key collision") {
				t.Fatalf("validate() error = %v, want stable bad fixture rejection", err)
			}
		})
	}
}

func TestValidateResolvedBindingContractAcceptsPreferredRemoteFetch(t *testing.T) {
	binding := spec.ArtifactBinding{BindingName: "dataset"}
	resolved := handoff.ResolveBindingResponse{
		ResolutionStatus: "RESOLVED",
		PlacementIntent: handoff.PlacementIntent{
			Mode:     placementModePreferredNode,
			NodeName: "worker-1",
		},
		MaterializationPlan: handoff.MaterializationPlan{
			Mode:           materializationModeRemoteFetch,
			URI:            "http://artifact.local/dataset",
			ExpectedDigest: "sha256:abc",
			LocalPath:      "inputs/dataset",
		},
	}

	if err := validateResolvedBindingContract(binding, resolved); err != nil {
		t.Fatalf("validateResolvedBindingContract() error = %v", err)
	}
}

func TestValidateResolvedBindingContractRejectsPlacementWithoutNode(t *testing.T) {
	binding := spec.ArtifactBinding{BindingName: "dataset"}
	resolved := handoff.ResolveBindingResponse{
		ResolutionStatus: "RESOLVED",
		PlacementIntent:  handoff.PlacementIntent{Mode: placementModeRequiredNode},
		MaterializationPlan: handoff.MaterializationPlan{
			Mode: materializationModeNone,
		},
	}

	if err := validateResolvedBindingContract(binding, resolved); err == nil {
		t.Fatal("expected placement contract error")
	}
}

func TestValidateResolvedBindingContractRejectsLocalReuseWithoutNodeLocalSource(t *testing.T) {
	binding := spec.ArtifactBinding{BindingName: "dataset"}
	resolved := handoff.ResolveBindingResponse{
		ResolutionStatus: "RESOLVED",
		MaterializationPlan: handoff.MaterializationPlan{
			Mode: materializationModeLocalReuse,
		},
	}

	if err := validateResolvedBindingContract(binding, resolved); err == nil {
		t.Fatal("expected local_reuse contract error")
	}
}

func TestValidateResolvedBindingContractRejectsRemoteFetchWithoutSource(t *testing.T) {
	binding := spec.ArtifactBinding{BindingName: "dataset"}
	resolved := handoff.ResolveBindingResponse{
		ResolutionStatus: "RESOLVED",
		MaterializationPlan: handoff.MaterializationPlan{
			Mode: materializationModeRemoteFetch,
		},
	}

	if err := validateResolvedBindingContract(binding, resolved); err == nil {
		t.Fatal("expected remote_fetch contract error")
	}
}

func TestValidateResolvedBindingContractRejectsUnsafeLocalPath(t *testing.T) {
	binding := spec.ArtifactBinding{BindingName: "dataset"}
	resolved := handoff.ResolveBindingResponse{
		ResolutionStatus: "RESOLVED",
		MaterializationPlan: handoff.MaterializationPlan{
			Mode:      materializationModeRemoteFetch,
			URI:       "http://artifact.local/dataset",
			LocalPath: "../escape",
		},
	}

	if err := validateResolvedBindingContract(binding, resolved); err == nil {
		t.Fatal("expected unsafe localPath contract error")
	}
}
