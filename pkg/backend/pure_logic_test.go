package backend

import (
	"strings"
	"testing"
	"time"

	"github.com/HeaInSeo/JUMI/pkg/provenance"
	"github.com/HeaInSeo/JUMI/pkg/spec"
	spapi "github.com/HeaInSeo/spawner/pkg/api"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// --- directSanitizeName ---

func TestDirectSanitizeName(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "simple lowercase",
			input: "run-1-node-a",
			want:  "run-1-node-a",
		},
		{
			name:  "uppercase converted to lowercase",
			input: "Run-1-Node-A",
			want:  "run-1-node-a",
		},
		{
			name:  "underscores become dashes",
			input: "run_1_node",
			want:  "run-1-node",
		},
		{
			name:  "leading and trailing dashes stripped",
			input: "_run-abc_",
			want:  "run-abc",
		},
		{
			name:  "special chars become dashes",
			input: "run/1/node",
			want:  "run-1-node",
		},
		{
			name:  "truncated to 63 chars",
			input: strings.Repeat("a", 70),
			want:  strings.Repeat("a", 63),
		},
		{
			name: "no trailing dash after truncation at 63",
			// 62 'a's + '_' → after sanitize: 62 'a's + '-' = 63 chars, trimmed to 62 (no trailing dash issue)
			input: strings.Repeat("a", 62) + "_",
			want:  strings.Repeat("a", 62),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := directSanitizeName(tc.input)
			if got != tc.want {
				t.Fatalf("directSanitizeName(%q) = %q, want %q", tc.input, got, tc.want)
			}
			if len(got) > 63 {
				t.Fatalf("result too long: %d chars", len(got))
			}
			if strings.HasSuffix(got, "-") {
				t.Fatalf("result has trailing dash: %q", got)
			}
		})
	}
}

// --- cleanupTTL ---

func TestCleanupTTL(t *testing.T) {
	if got := cleanupTTL(spec.Node{}); got != 600 {
		t.Fatalf("cleanupTTL(zero) = %d, want 600", got)
	}
	if got := cleanupTTL(spec.Node{CleanupPolicy: spec.CleanupPolicy{TTLSecondsAfterFinished: 1800}}); got != 1800 {
		t.Fatalf("cleanupTTL(1800) = %d, want 1800", got)
	}
}

// --- extractQueueName ---

func TestExtractQueueName(t *testing.T) {
	if got := extractQueueName(nil); got != "" {
		t.Fatalf("extractQueueName(nil) = %q, want empty", got)
	}
	if got := extractQueueName(map[string]string{"other": "v"}); got != "" {
		t.Fatalf("extractQueueName(no queue label) = %q, want empty", got)
	}
	if got := extractQueueName(map[string]string{"kueue.x-k8s.io/queue-name": "standard"}); got != "standard" {
		t.Fatalf("extractQueueName() = %q, want standard", got)
	}
}

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

// --- directHostPathSource ---

func TestDirectHostPathSource(t *testing.T) {
	path, ok := directHostPathSource("hostpath:/var/lib/data")
	if !ok {
		t.Fatal("expected hostpath source to be recognized")
	}
	if path != "/var/lib/data" {
		t.Fatalf("path = %q, want /var/lib/data", path)
	}

	_, ok2 := directHostPathSource("pvcname")
	if ok2 {
		t.Fatal("expected pvcname not to be recognized as hostpath")
	}

	_, ok3 := directHostPathSource("hostpath:")
	if ok3 {
		t.Fatal("expected empty hostpath suffix not to be recognized")
	}
}

// --- buildDirectNodeSelector with empty placement ---

func TestBuildDirectNodeSelector_NilPlacement(t *testing.T) {
	if got := buildDirectNodeSelector(nil); got != nil {
		t.Fatalf("buildDirectNodeSelector(nil) = %v, want nil", got)
	}
}

func TestBuildDirectNodeSelector_EmptyPlacement(t *testing.T) {
	if got := buildDirectNodeSelector(&spapi.Placement{}); got != nil {
		t.Fatalf("buildDirectNodeSelector(empty) = %v, want nil", got)
	}
}

// --- buildDirectAffinity with nil / empty placement ---

func TestBuildDirectAffinity_Nil(t *testing.T) {
	if got := buildDirectAffinity(nil); got != nil {
		t.Fatalf("buildDirectAffinity(nil) = %v, want nil", got)
	}
}

func TestBuildDirectAffinity_EmptyPreferred(t *testing.T) {
	if got := buildDirectAffinity(&spapi.Placement{}); got != nil {
		t.Fatalf("buildDirectAffinity(no preferred) = %v, want nil", got)
	}
}

// --- buildPlacement edge cases ---

func TestBuildPlacement_Nil(t *testing.T) {
	if got := buildPlacement(spec.Node{}); got != nil {
		t.Fatalf("buildPlacement(nil hints) = %v, want nil", got)
	}
}

func TestBuildPlacement_EmptyHints(t *testing.T) {
	node := spec.Node{Placement: &spec.PlacementHints{}}
	if got := buildPlacement(node); got != nil {
		t.Fatalf("buildPlacement(empty hints) = %v, want nil (all empty)", got)
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

// --- directJobSucceeded / directJobFailed / directJobFailureMessage ---

func makeBatchJob(succeeded bool, failed bool, reason string) *batchv1.Job {
	return makeBatchJobWithMessage(succeeded, failed, reason, "")
}

func makeBatchJobWithMessage(succeeded bool, failed bool, reason, message string) *batchv1.Job {
	job := &batchv1.Job{}
	if succeeded {
		job.Status.Conditions = append(job.Status.Conditions, batchv1.JobCondition{
			Type:   batchv1.JobComplete,
			Status: corev1.ConditionTrue,
			Reason: reason,
		})
	}
	if failed {
		job.Status.Conditions = append(job.Status.Conditions, batchv1.JobCondition{
			Type:    batchv1.JobFailed,
			Status:  corev1.ConditionTrue,
			Reason:  reason,
			Message: message,
		})
	}
	return job
}

func TestDirectJobSucceeded(t *testing.T) {
	if !directJobSucceeded(makeBatchJob(true, false, "")) {
		t.Fatal("directJobSucceeded(complete) = false, want true")
	}
	if directJobSucceeded(makeBatchJob(false, true, "error")) {
		t.Fatal("directJobSucceeded(failed) = true, want false")
	}
}

func TestDirectJobFailed(t *testing.T) {
	if !directJobFailed(makeBatchJob(false, true, "")) {
		t.Fatal("directJobFailed(failed) = false, want true")
	}
	if directJobFailed(makeBatchJob(true, false, "")) {
		t.Fatal("directJobFailed(complete) = true, want false")
	}
}

func TestDirectJobFailureMessage(t *testing.T) {
	if got := directJobFailureMessage(makeBatchJob(false, false, "")); got != "" {
		t.Fatalf("directJobFailureMessage(no condition) = %q, want empty", got)
	}
	if got := directJobFailureMessage(makeBatchJobWithMessage(false, true, "BackoffLimitExceeded", "pod failed")); got != "pod failed" {
		t.Fatalf("directJobFailureMessage(with message) = %q, want 'pod failed'", got)
	}
	// Falls back to reason when message is blank.
	if got := directJobFailureMessage(makeBatchJobWithMessage(false, true, "BackoffLimitExceeded", "")); got != "BackoffLimitExceeded" {
		t.Fatalf("directJobFailureMessage(no message) = %q, want reason", got)
	}
}
