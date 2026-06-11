package backend

import (
	"strings"
	"testing"
	"time"

	"github.com/HeaInSeo/JUMI/pkg/provenance"
	"github.com/HeaInSeo/JUMI/pkg/spec"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// --- manifestPathForNode ---

func TestManifestPathForNode(t *testing.T) {
	defaultPath := provenance.DefaultArtifactsManifestPath

	if got := manifestPathForNode(spec.Node{}); got != defaultPath {
		t.Fatalf("manifestPathForNode(no env) = %q, want default %q", got, defaultPath)
	}
	if got := manifestPathForNode(spec.Node{Env: map[string]string{"OTHER": "x"}}); got != defaultPath {
		t.Fatalf("manifestPathForNode(no manifest path env) = %q, want default %q", got, defaultPath)
	}
	custom := "/custom/manifest.json"
	if got := manifestPathForNode(spec.Node{Env: map[string]string{"JUMI_OUTPUT_MANIFEST_PATH": custom}}); got != custom {
		t.Fatalf("manifestPathForNode(custom) = %q, want %q", got, custom)
	}
}

// --- manifestExportMode ---

func TestManifestExportMode(t *testing.T) {
	if got := manifestExportMode(spec.Node{}); got != "" {
		t.Fatalf("manifestExportMode(no outputs) = %q, want empty", got)
	}
	if got := manifestExportMode(spec.Node{Outputs: []string{"out"}}); got != "" {
		t.Fatalf("manifestExportMode(no metadata) = %q, want empty", got)
	}
	if got := manifestExportMode(spec.Node{
		Outputs:  []string{"out"},
		Metadata: map[string]string{"jumi.outputManifestMode": "wrapped-shell"},
	}); got != "wrapped-shell" {
		t.Fatalf("manifestExportMode(wrapped-shell) = %q, want wrapped-shell", got)
	}
	if got := manifestExportMode(spec.Node{
		Outputs:  []string{"out"},
		Metadata: map[string]string{"jumi.outputManifestMode": "runtime-helper"},
	}); got != "runtime-helper" {
		t.Fatalf("manifestExportMode(runtime-helper) = %q, want runtime-helper", got)
	}
	if got := manifestExportMode(spec.Node{
		Outputs:  []string{"out"},
		Metadata: map[string]string{"jumi.outputManifestMode": "unknown"},
	}); got != "" {
		t.Fatalf("manifestExportMode(unknown) = %q, want empty", got)
	}
}

// --- outputProducerAttemptID ---

func TestOutputProducerAttemptID(t *testing.T) {
	manifest := provenance.ArtifactManifest{AttemptID: "manifest-attempt"}
	record := provenance.ArtifactRecord{ProducerAttemptID: "record-attempt"}
	if got := outputProducerAttemptID(record, manifest); got != "record-attempt" {
		t.Fatalf("outputProducerAttemptID(record has it) = %q, want record-attempt", got)
	}

	record2 := provenance.ArtifactRecord{ProducerAttemptID: ""}
	if got := outputProducerAttemptID(record2, manifest); got != "manifest-attempt" {
		t.Fatalf("outputProducerAttemptID(fallback to manifest) = %q, want manifest-attempt", got)
	}

	record3 := provenance.ArtifactRecord{ProducerAttemptID: "   "}
	if got := outputProducerAttemptID(record3, manifest); got != "manifest-attempt" {
		t.Fatalf("outputProducerAttemptID(whitespace only) = %q, want manifest-attempt", got)
	}
}

// --- mainContainerSucceeded / mainContainerTerminated ---

func TestMainContainerSucceeded(t *testing.T) {
	if mainContainerSucceeded(nil) {
		t.Fatal("mainContainerSucceeded(nil) = true, want false")
	}
	pod := &corev1.Pod{
		Status: corev1.PodStatus{ContainerStatuses: []corev1.ContainerStatus{
			{Name: "main", State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{ExitCode: 0}}},
		}},
	}
	if !mainContainerSucceeded(pod) {
		t.Fatal("mainContainerSucceeded(exit 0) = false, want true")
	}
	pod.Status.ContainerStatuses[0].State.Terminated.ExitCode = 1
	if mainContainerSucceeded(pod) {
		t.Fatal("mainContainerSucceeded(exit 1) = true, want false")
	}
	// No "main" container.
	pod2 := &corev1.Pod{
		Status: corev1.PodStatus{ContainerStatuses: []corev1.ContainerStatus{
			{Name: "sidecar", State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{ExitCode: 0}}},
		}},
	}
	if mainContainerSucceeded(pod2) {
		t.Fatal("mainContainerSucceeded(no main container) = true, want false")
	}
}

func TestMainContainerTerminated(t *testing.T) {
	if mainContainerTerminated(nil) {
		t.Fatal("mainContainerTerminated(nil) = true, want false")
	}
	pod := &corev1.Pod{
		Status: corev1.PodStatus{ContainerStatuses: []corev1.ContainerStatus{
			{Name: "main", State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{ExitCode: 1}}},
		}},
	}
	if !mainContainerTerminated(pod) {
		t.Fatal("mainContainerTerminated(terminated) = false, want true")
	}
	pod2 := &corev1.Pod{
		Status: corev1.PodStatus{ContainerStatuses: []corev1.ContainerStatus{
			{Name: "main"},
		}},
	}
	if mainContainerTerminated(pod2) {
		t.Fatal("mainContainerTerminated(running) = true, want false")
	}
}

// --- terminatedAt / podSucceededLater / podTerminatedLater ---

func TestTerminatedAt(t *testing.T) {
	if !terminatedAt(nil).IsZero() {
		t.Fatal("terminatedAt(nil) should be zero")
	}
	pod := &corev1.Pod{
		Status: corev1.PodStatus{ContainerStatuses: []corev1.ContainerStatus{
			{Name: "sidecar"}, // no main
		}},
	}
	if !terminatedAt(pod).IsZero() {
		t.Fatal("terminatedAt(no main) should be zero")
	}
	ts := time.Now().UTC()
	pod2 := &corev1.Pod{
		Status: corev1.PodStatus{ContainerStatuses: []corev1.ContainerStatus{
			{Name: "main", State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{
				FinishedAt: metav1.NewTime(ts),
			}}},
		}},
	}
	if got := terminatedAt(pod2); !got.Equal(ts) {
		t.Fatalf("terminatedAt() = %v, want %v", got, ts)
	}
}

func TestPodSucceededLater(t *testing.T) {
	earlier := time.Now().UTC()
	later := earlier.Add(time.Second)
	makeTerminatedPod := func(ts time.Time) *corev1.Pod {
		return &corev1.Pod{
			Status: corev1.PodStatus{ContainerStatuses: []corev1.ContainerStatus{
				{Name: "main", State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{
					FinishedAt: metav1.NewTime(ts),
				}}},
			}},
		}
	}
	podEarly := makeTerminatedPod(earlier)
	podLate := makeTerminatedPod(later)

	if !podSucceededLater(podLate, podEarly) {
		t.Fatal("podSucceededLater(late, early) = false, want true")
	}
	if podSucceededLater(podEarly, podLate) {
		t.Fatal("podSucceededLater(early, late) = true, want false")
	}
}

// --- readArtifactsManifestFromTerminationMessage edge cases ---

func TestReadArtifactsManifestFromTerminationMessage_Nil(t *testing.T) {
	raw, err := readArtifactsManifestFromTerminationMessage(nil)
	if err != nil {
		t.Fatalf("readArtifactsManifestFromTerminationMessage(nil) error = %v", err)
	}
	if raw != nil {
		t.Fatalf("expected nil result for nil pod, got %q", raw)
	}
}

func TestReadArtifactsManifestFromTerminationMessage_LastTerminationState(t *testing.T) {
	msg := `{"artifacts":[]}`
	pod := &corev1.Pod{
		Status: corev1.PodStatus{ContainerStatuses: []corev1.ContainerStatus{
			{
				Name: "main",
				// State has nil Terminated
				LastTerminationState: corev1.ContainerState{
					Terminated: &corev1.ContainerStateTerminated{Message: msg},
				},
			},
		}},
	}
	raw, err := readArtifactsManifestFromTerminationMessage(pod)
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if string(raw) != msg {
		t.Fatalf("got %q, want %q", string(raw), msg)
	}
}

// --- contractFirstNonEmpty ---

func TestContractFirstNonEmpty(t *testing.T) {
	if got := contractFirstNonEmpty("", "  ", "third"); got != "third" {
		t.Fatalf("contractFirstNonEmpty = %q, want third", got)
	}
	if got := contractFirstNonEmpty("", ""); got != "" {
		t.Fatalf("contractFirstNonEmpty(all empty) = %q, want empty", got)
	}
}

// --- cloneEnv ---

func TestCloneEnv(t *testing.T) {
	if got := cloneEnv(nil); len(got) != 0 {
		t.Fatalf("cloneEnv(nil) = %v, want empty map", got)
	}
	src := map[string]string{"A": "1", "B": "2"}
	dst := cloneEnv(src)
	if dst["A"] != "1" || dst["B"] != "2" {
		t.Fatalf("cloneEnv() = %v, missing entries", dst)
	}
	dst["A"] = "mutated"
	if src["A"] == "mutated" {
		t.Fatal("cloneEnv() did not deep-copy")
	}
}

// --- injectOutputContractEnv ---

func TestInjectOutputContractEnv_NoOutputs(t *testing.T) {
	env := map[string]string{}
	injectOutputContractEnv(env, spec.RunRecord{RunID: "run-1"}, spec.Node{NodeID: "n", Image: "img:1"})
	if _, ok := env["JUMI_OUTPUT_MANIFEST_ENABLED"]; ok {
		t.Fatal("should not inject contract env when node has no outputs")
	}
}

func TestInjectOutputContractEnv_WithOutputs(t *testing.T) {
	env := map[string]string{}
	run := spec.RunRecord{RunID: "run-1", Spec: spec.ExecutableRunSpec{Run: spec.RunMetadata{SampleRunID: "sample-1"}}}
	node := spec.Node{NodeID: "n", Image: "img:1", Outputs: []string{"report.json", "logs.txt"}}
	injectOutputContractEnv(env, run, node)
	if env["JUMI_OUTPUT_MANIFEST_ENABLED"] != "true" {
		t.Fatalf("JUMI_OUTPUT_MANIFEST_ENABLED = %q, want true", env["JUMI_OUTPUT_MANIFEST_ENABLED"])
	}
	if env["JUMI_RUN_ID"] != "run-1" {
		t.Fatalf("JUMI_RUN_ID = %q, want run-1", env["JUMI_RUN_ID"])
	}
	if env["JUMI_NODE_ID"] != "n" {
		t.Fatalf("JUMI_NODE_ID = %q, want n", env["JUMI_NODE_ID"])
	}
	if env["JUMI_SAMPLE_RUN_ID"] != "sample-1" {
		t.Fatalf("JUMI_SAMPLE_RUN_ID = %q, want sample-1", env["JUMI_SAMPLE_RUN_ID"])
	}
}

func TestInjectOutputContractEnv_DoesNotOverrideExisting(t *testing.T) {
	env := map[string]string{"JUMI_OUTPUT_ROOT": "/custom-out"}
	run := spec.RunRecord{RunID: "run-1"}
	node := spec.Node{NodeID: "n", Image: "img:1", Outputs: []string{"out"}}
	injectOutputContractEnv(env, run, node)
	if env["JUMI_OUTPUT_ROOT"] != "/custom-out" {
		t.Fatalf("JUMI_OUTPUT_ROOT overridden: got %q, want /custom-out", env["JUMI_OUTPUT_ROOT"])
	}
}

func TestInjectOutputContractEnv_EmptyOutputsSkipped(t *testing.T) {
	env := map[string]string{}
	run := spec.RunRecord{RunID: "run-1"}
	node := spec.Node{NodeID: "n", Image: "img:1", Outputs: []string{"", ""}}
	injectOutputContractEnv(env, run, node)
	if _, ok := env["JUMI_OUTPUT_MANIFEST_ENABLED"]; ok {
		t.Fatal("should not inject when all outputs are empty strings")
	}
}

// --- stripContractInputEnv ---

func TestStripContractInputEnv(t *testing.T) {
	env := map[string]string{
		"JUMI_INPUT_FOO_URI":                  "http://foo.local/file",
		"JUMI_INPUT_FOO_EXPECTED_DIGEST":      "sha256:abc",
		"JUMI_INPUT_FOO_EXPECTED_SIZE_BYTES":  "42",
		"JUMI_INPUT_FOO_MATERIALIZATION_MODE": "remote_fetch",
		"JUMI_INPUT_FOO_NODE_LOCAL_PATH":      "/cache/foo",
		"JUMI_INPUT_FOO_LOCAL_PATH":           "/work/inputs/foo",
		"JUMI_RUN_ID":                         "run-1", // should NOT be stripped
		"OTHER_VAR":                           "val",   // should NOT be stripped
	}
	stripContractInputEnv(env)
	for _, stripped := range []string{
		"JUMI_INPUT_FOO_URI",
		"JUMI_INPUT_FOO_EXPECTED_DIGEST",
		"JUMI_INPUT_FOO_EXPECTED_SIZE_BYTES",
		"JUMI_INPUT_FOO_MATERIALIZATION_MODE",
		"JUMI_INPUT_FOO_NODE_LOCAL_PATH",
		"JUMI_INPUT_FOO_LOCAL_PATH",
	} {
		if _, ok := env[stripped]; ok {
			t.Fatalf("expected %q to be stripped, still present", stripped)
		}
	}
	for _, kept := range []string{"JUMI_RUN_ID", "OTHER_VAR"} {
		if _, ok := env[kept]; !ok {
			t.Fatalf("expected %q to be retained, was stripped", kept)
		}
	}
}

// --- buildNodeContractInputs ---

func TestBuildNodeContractInputs_Empty(t *testing.T) {
	if got := buildNodeContractInputs(nil); got != nil {
		t.Fatalf("buildNodeContractInputs(nil) = %v, want nil", got)
	}
	if got := buildNodeContractInputs(map[string]string{}); got != nil {
		t.Fatalf("buildNodeContractInputs(empty) = %v, want nil", got)
	}
}

func TestBuildNodeContractInputs_SkipsNoneMode(t *testing.T) {
	env := map[string]string{
		"JUMI_INPUT_FOO_URI":                  "http://foo.local/file",
		"JUMI_INPUT_FOO_MATERIALIZATION_MODE": "none",
	}
	if got := buildNodeContractInputs(env); got != nil {
		t.Fatalf("buildNodeContractInputs(mode=none) = %v, want nil (skipped)", got)
	}
}

func TestBuildNodeContractInputs_SkipsEmptyMode(t *testing.T) {
	env := map[string]string{
		"JUMI_INPUT_FOO_URI": "http://foo.local/file",
		// No MATERIALIZATION_MODE => empty => skipped
	}
	if got := buildNodeContractInputs(env); got != nil {
		t.Fatalf("buildNodeContractInputs(no mode) = %v, want nil (skipped)", got)
	}
}

func TestBuildNodeContractInputs_FullInput(t *testing.T) {
	env := map[string]string{
		"JUMI_INPUT_DATASET_URI":                  "http://foo.local/file",
		"JUMI_INPUT_DATASET_EXPECTED_DIGEST":      "sha256:abc",
		"JUMI_INPUT_DATASET_EXPECTED_SIZE_BYTES":  "100",
		"JUMI_INPUT_DATASET_MATERIALIZATION_MODE": "remote_fetch",
		"JUMI_INPUT_DATASET_LOCAL_PATH":           "/work/inputs/dataset",
		"JUMI_INPUT_DATASET_NODE_LOCAL_PATH":      "/cache/dataset",
	}
	inputs := buildNodeContractInputs(env)
	if len(inputs) != 1 {
		t.Fatalf("len = %d, want 1", len(inputs))
	}
	in := inputs[0]
	if in.Name != "dataset" {
		t.Fatalf("Name = %q, want dataset", in.Name)
	}
	if in.URI != "http://foo.local/file" {
		t.Fatalf("URI = %q, want http://foo.local/file", in.URI)
	}
	if in.ExpectedDigest != "sha256:abc" {
		t.Fatalf("ExpectedDigest = %q, want sha256:abc", in.ExpectedDigest)
	}
	if in.ExpectedSizeBytes != 100 {
		t.Fatalf("ExpectedSizeBytes = %d, want 100", in.ExpectedSizeBytes)
	}
	if in.MaterializationMode != "remote_fetch" {
		t.Fatalf("MaterializationMode = %q, want remote_fetch", in.MaterializationMode)
	}
	if in.LocalPath != "/work/inputs/dataset" {
		t.Fatalf("LocalPath = %q, want /work/inputs/dataset", in.LocalPath)
	}
	if in.NodeLocalPath != "/cache/dataset" {
		t.Fatalf("NodeLocalPath = %q, want /cache/dataset", in.NodeLocalPath)
	}
}

// --- setEnvDefault ---

func TestSetEnvDefault(t *testing.T) {
	env := map[string]string{}
	setEnvDefault(env, "KEY", "value")
	if env["KEY"] != "value" {
		t.Fatalf("setEnvDefault: env[KEY] = %q, want value", env["KEY"])
	}
	// Should not overwrite existing value.
	setEnvDefault(env, "KEY", "other")
	if env["KEY"] != "value" {
		t.Fatalf("setEnvDefault overrode existing value")
	}
}

// --- validateObservedManifest - missing ID paths ---

func TestValidateObservedManifest_MissingRunID(t *testing.T) {
	node := spec.Node{Env: map[string]string{"JUMI_RUN_ID": "run-1"}}
	manifest := provenance.ArtifactManifest{RunID: ""}
	if err := validateObservedManifest(manifest, node); err == nil {
		t.Fatal("expected error for missing manifest runId")
	}
}

func TestValidateObservedManifest_RunIDMismatch(t *testing.T) {
	node := spec.Node{Env: map[string]string{"JUMI_RUN_ID": "run-1"}}
	manifest := provenance.ArtifactManifest{RunID: "run-different"}
	if err := validateObservedManifest(manifest, node); err == nil {
		t.Fatal("expected error for runId mismatch")
	}
}

func TestValidateObservedManifest_MissingNodeID(t *testing.T) {
	node := spec.Node{Env: map[string]string{"JUMI_NODE_ID": "node-1"}}
	manifest := provenance.ArtifactManifest{NodeID: ""}
	if err := validateObservedManifest(manifest, node); err == nil {
		t.Fatal("expected error for missing manifest nodeId")
	}
}

func TestValidateObservedManifest_MissingAttemptID(t *testing.T) {
	node := spec.Node{Env: map[string]string{"JUMI_ATTEMPT_ID": "attempt-1"}}
	manifest := provenance.ArtifactManifest{AttemptID: ""}
	if err := validateObservedManifest(manifest, node); err == nil {
		t.Fatal("expected error for missing manifest attemptId")
	}
}

func TestValidateObservedManifest_NoEnv(t *testing.T) {
	node := spec.Node{} // no env map
	manifest := provenance.ArtifactManifest{}
	if err := validateObservedManifest(manifest, node); err != nil {
		t.Fatalf("validateObservedManifest(no env) error = %v, want nil", err)
	}
}

// --- wrapCommandForManifestExport ---

func TestWrapCommandForManifestExport_NoOutputs(t *testing.T) {
	node := spec.Node{} // no outputs => no wrapping
	cmd := []string{"python", "app.py"}
	got := wrapCommandForManifestExport(cmd, node)
	if len(got) != 2 || got[0] != "python" {
		t.Fatalf("wrapCommandForManifestExport(no outputs) = %v, want unchanged", got)
	}
}

func TestWrapCommandForManifestExport_WrappedShell(t *testing.T) {
	node := spec.Node{
		Outputs:  []string{"out.txt"},
		Metadata: map[string]string{"jumi.outputManifestMode": "wrapped-shell"},
	}
	cmd := []string{"python", "app.py"}
	got := wrapCommandForManifestExport(cmd, node)
	if len(got) < 4 {
		t.Fatalf("wrapped command too short: %v", got)
	}
	if got[0] != "/bin/sh" || got[1] != "-ceu" {
		t.Fatalf("wrapped prefix = %v, want [/bin/sh -ceu ...]", got[:2])
	}
	if !strings.Contains(got[2], "JUMI_OUTPUT_MANIFEST_PATH") {
		t.Fatalf("wrapper script missing JUMI_OUTPUT_MANIFEST_PATH reference: %q", got[2])
	}
}

func TestWrapCommandForManifestExport_RuntimeHelper(t *testing.T) {
	node := spec.Node{
		Outputs:  []string{"report"},
		Metadata: map[string]string{"jumi.outputManifestMode": "runtime-helper"},
	}
	cmd := []string{"sh", "-c", "echo hi"}
	got := wrapCommandForManifestExport(cmd, node)
	if len(got) < 4 {
		t.Fatalf("runtime-helper command too short: %v", got)
	}
	if got[0] != "/bin/sh" || got[1] != "-ceu" {
		t.Fatalf("runtime-helper prefix = %v, want [/bin/sh -ceu ...]", got[:2])
	}
	if !strings.Contains(got[2], "node-contract.json") {
		t.Fatalf("runtime-helper wrapper missing contract path: %q", got[2])
	}
}
