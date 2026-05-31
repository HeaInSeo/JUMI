package backend

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/HeaInSeo/JUMI/pkg/provenance"
	"github.com/HeaInSeo/JUMI/pkg/spec"
	spapi "github.com/HeaInSeo/spawner/pkg/api"
	corev1 "k8s.io/api/core/v1"
)

func TestToSpawnerRunSpecMapsRuntimeContractFields(t *testing.T) {
	run := spec.RunRecord{
		RunID: "run-1",
		Spec: spec.ExecutableRunSpec{
			Run: spec.RunMetadata{TraceID: "trace-1"},
		},
	}
	node := spec.Node{
		NodeID:             "worker",
		Image:              "busybox:1.36",
		Command:            []string{"sh"},
		Args:               []string{"-c", "echo hi"},
		WorkingDir:         "/workspace",
		ServiceAccountName: "jumi-runner",
		ResourceProfile:    spec.ResourceProfile{CPU: "250m", Memory: "128Mi"},
		Outputs:            []string{"logs.txt", "result.json"},
		CleanupPolicy:      spec.CleanupPolicy{TTLSecondsAfterFinished: 1800},
		Placement: &spec.PlacementHints{
			NodeSelector: map[string]string{"kubernetes.io/hostname": "lab-worker-1"},
		},
	}

	got := toSpawnerRunSpec(run, node)

	if value, ok := optionalStringField(got, "WorkingDir"); ok && value != "/workspace" {
		t.Fatalf("WorkingDir = %q, want /workspace", value)
	}
	if value, ok := optionalStringField(got, "ServiceAccountName"); ok && value != "jumi-runner" {
		t.Fatalf("ServiceAccountName = %q, want jumi-runner", value)
	}
	if got.Cleanup.TTLSecondsAfterFinished != 1800 {
		t.Fatalf("Cleanup TTL = %d, want 1800", got.Cleanup.TTLSecondsAfterFinished)
	}
	if got.Placement == nil {
		t.Fatal("Placement = nil, want node selector")
	}
	if got.Placement.NodeSelector["kubernetes.io/hostname"] != "lab-worker-1" {
		t.Fatalf("NodeSelector hostname = %q, want lab-worker-1", got.Placement.NodeSelector["kubernetes.io/hostname"])
	}
	if got.Env["JUMI_OUTPUT_MANIFEST_ENABLED"] != "true" {
		t.Fatalf("JUMI_OUTPUT_MANIFEST_ENABLED = %q, want true", got.Env["JUMI_OUTPUT_MANIFEST_ENABLED"])
	}
	if got.Env["JUMI_OUTPUT_MANIFEST_PATH"] != "/out/_meta/artifacts.manifest.json" {
		t.Fatalf("JUMI_OUTPUT_MANIFEST_PATH = %q, want /out/_meta/artifacts.manifest.json", got.Env["JUMI_OUTPUT_MANIFEST_PATH"])
	}
	if got.Env["JUMI_OUTPUT_NAMES"] != "logs.txt,result.json" {
		t.Fatalf("JUMI_OUTPUT_NAMES = %q, want logs.txt,result.json", got.Env["JUMI_OUTPUT_NAMES"])
	}
	if got.Env["JUMI_RUN_ID"] != "run-1" {
		t.Fatalf("JUMI_RUN_ID = %q, want run-1", got.Env["JUMI_RUN_ID"])
	}
	if got.Env["JUMI_OUTPUT_ROOT"] != "/out" {
		t.Fatalf("JUMI_OUTPUT_ROOT = %q, want /out", got.Env["JUMI_OUTPUT_ROOT"])
	}
}

func TestToSpawnerRunSpecMapsPreferredPlacementFields(t *testing.T) {
	run := spec.RunRecord{RunID: "run-placement"}
	node := spec.Node{
		NodeID:  "worker",
		Image:   "busybox:1.36",
		Command: []string{"echo", "hi"},
		Placement: &spec.PlacementHints{
			PreferredNodes: []spec.WeightedNodePreference{
				{NodeName: "lab-worker-1", Weight: 100},
				{NodeName: "lab-worker-2", Weight: 50},
			},
		},
	}

	got := toSpawnerRunSpec(run, node)

	if got.Placement == nil {
		t.Fatal("Placement = nil, want preferred nodes")
	}
	if len(got.Placement.PreferredNodes) != 2 {
		t.Fatalf("PreferredNodes = %d, want 2", len(got.Placement.PreferredNodes))
	}
	if got.Placement.PreferredNodes[0].NodeName != "lab-worker-1" || got.Placement.PreferredNodes[0].Weight != 100 {
		t.Fatalf("unexpected first preferred node: %+v", got.Placement.PreferredNodes[0])
	}
	if got.Placement.PreferredNodes[1].NodeName != "lab-worker-2" || got.Placement.PreferredNodes[1].Weight != 50 {
		t.Fatalf("unexpected second preferred node: %+v", got.Placement.PreferredNodes[1])
	}
}

func TestBuildDirectNodeSelectorMapsRequiredNodeName(t *testing.T) {
	got := buildDirectNodeSelector(&spapi.Placement{
		NodeSelector:     map[string]string{"topology.kubernetes.io/zone": "lab-a"},
		RequiredNodeName: "lab-worker-1",
	})

	if got["topology.kubernetes.io/zone"] != "lab-a" {
		t.Fatalf("zone selector = %q, want lab-a", got["topology.kubernetes.io/zone"])
	}
	if got["kubernetes.io/hostname"] != "lab-worker-1" {
		t.Fatalf("hostname selector = %q, want lab-worker-1", got["kubernetes.io/hostname"])
	}
}

func TestBuildDirectAffinityMapsPreferredNodes(t *testing.T) {
	got := buildDirectAffinity(&spapi.Placement{
		PreferredNodes: []spapi.WeightedNodePreference{
			{NodeName: "lab-worker-1", Weight: 100},
			{NodeName: "lab-worker-2", Weight: 50},
		},
	})

	if got == nil || got.NodeAffinity == nil {
		t.Fatal("Affinity = nil, want preferred node affinity")
	}
	terms := got.NodeAffinity.PreferredDuringSchedulingIgnoredDuringExecution
	if len(terms) != 2 {
		t.Fatalf("preferred affinity terms = %d, want 2", len(terms))
	}
	if terms[0].Weight != 100 || terms[0].Preference.MatchExpressions[0].Values[0] != "lab-worker-1" {
		t.Fatalf("unexpected first preferred term: %+v", terms[0])
	}
	if terms[1].Weight != 50 || terms[1].Preference.MatchExpressions[0].Values[0] != "lab-worker-2" {
		t.Fatalf("unexpected second preferred term: %+v", terms[1])
	}
}

func TestToSpawnerRunSpecMapsServiceAccountFromSmokeFixtureStyleNode(t *testing.T) {
	run := spec.RunRecord{RunID: "run-fixture"}
	node := spec.Node{
		NodeID: "produce",
		// Test shortcut only:
		// this uses the JUMI image as a node runtime image because the current
		// smoke image still carries the legacy helper binary for compatibility.
		Image:              "harbor.10.113.24.96.nip.io/batch-int/jumi:test",
		Command:            []string{"sh", "-c", "echo hi"},
		ServiceAccountName: "jumi",
		Outputs:            []string{"report"},
		Metadata: map[string]string{
			"jumi.outputManifestMode": "runtime-helper",
		},
	}

	got := toSpawnerRunSpec(run, node)

	if value, ok := optionalStringField(got, "ServiceAccountName"); ok && value != "jumi" {
		t.Fatalf("ServiceAccountName = %q, want jumi", value)
	}
	if got.Command[0] != "/bin/sh" || got.Command[1] != "-ceu" {
		t.Fatalf("runtime-helper command prefix = %q, want [/bin/sh -ceu]", got.Command[:2])
	}
	if !strings.Contains(got.Command[2], ArtifactHelperPath+"\" run --contract") && !strings.Contains(got.Command[2], ArtifactHelperPath+" run --contract") {
		t.Fatalf("runtime-helper wrapper missing contract execution: %q", got.Command[2])
	}
}

func TestToSpawnerRunSpecUsesConfiguredArtifactHelperPath(t *testing.T) {
	t.Setenv(ArtifactHelperPathEnv, ArtifactHelperPath)
	run := spec.RunRecord{RunID: "run-fixture"}
	node := spec.Node{
		NodeID:   "produce",
		Image:    "helper-image:test",
		Command:  []string{"sh", "-c", "echo hi"},
		Outputs:  []string{"report"},
		Metadata: map[string]string{"jumi.outputManifestMode": "runtime-helper"},
	}

	got := toSpawnerRunSpec(run, node)

	if got.Command[0] != "/bin/sh" || got.Command[1] != "-ceu" {
		t.Fatalf("runtime-helper command prefix = %q, want [/bin/sh -ceu]", got.Command[:2])
	}
	if !strings.Contains(got.Command[2], ArtifactHelperPath+"\" run --contract") && !strings.Contains(got.Command[2], ArtifactHelperPath+" run --contract") {
		t.Fatalf("runtime-helper wrapper missing contract execution: %q", got.Command[2])
	}
}

func TestToSpawnerRunSpecUsesDefaultCleanupTTL(t *testing.T) {
	got := toSpawnerRunSpec(spec.RunRecord{RunID: "run-2"}, spec.Node{
		NodeID: "worker",
		Image:  "busybox:1.36",
	})

	if got.Cleanup.TTLSecondsAfterFinished != 600 {
		t.Fatalf("Cleanup TTL = %d, want 600", got.Cleanup.TTLSecondsAfterFinished)
	}
}

func TestToSpawnerRunSpecWrapsCommandForManifestExportWhenOptedIn(t *testing.T) {
	run := spec.RunRecord{RunID: "run-3", Spec: spec.ExecutableRunSpec{Run: spec.RunMetadata{SampleRunID: "sample-3"}}}
	node := spec.Node{
		NodeID:   "worker",
		Image:    "busybox:1.36",
		Command:  []string{"python"},
		Args:     []string{"app.py"},
		Outputs:  []string{"result.json"},
		Metadata: map[string]string{"jumi.outputManifestMode": "wrapped-shell"},
	}

	got := toSpawnerRunSpec(run, node)

	if len(got.Command) < 6 {
		t.Fatalf("wrapped command length = %d, want >= 6", len(got.Command))
	}
	if got.Command[0] != "/bin/sh" || got.Command[1] != "-ceu" {
		t.Fatalf("wrapped command prefix = %q, want [/bin/sh -ceu]", got.Command[:2])
	}
	if !strings.Contains(got.Command[2], "JUMI_OUTPUT_MANIFEST_PATH") {
		t.Fatalf("wrapper script missing manifest env reference: %q", got.Command[2])
	}
	if !strings.Contains(got.Command[2], "/dev/termination-log") {
		t.Fatalf("wrapper script missing termination-log export: %q", got.Command[2])
	}
	if got.Command[4] != "python" || got.Command[5] != "app.py" {
		t.Fatalf("wrapped original command = %q, want [python app.py]", got.Command[4:])
	}
}

func TestToSpawnerRunSpecWrapsCommandForRuntimeHelperMode(t *testing.T) {
	run := spec.RunRecord{RunID: "run-4", Spec: spec.ExecutableRunSpec{Run: spec.RunMetadata{SampleRunID: "sample-4"}}}
	node := spec.Node{
		NodeID:   "worker",
		Image:    "helper-image:latest",
		Command:  []string{"sh"},
		Args:     []string{"-c", "echo hi > /out/report"},
		Outputs:  []string{"report"},
		Metadata: map[string]string{"jumi.outputManifestMode": "runtime-helper"},
		Env: map[string]string{
			"JUMI_INPUT_DATASET_URI":                  "http://artifact.local/dataset",
			"JUMI_INPUT_DATASET_EXPECTED_DIGEST":      "sha256:abc",
			"JUMI_INPUT_DATASET_EXPECTED_SIZE_BYTES":  "17",
			"JUMI_INPUT_DATASET_MATERIALIZATION_MODE": "remote_fetch",
			"JUMI_INPUT_DATASET_LOCAL_PATH":           "inputs/result",
		},
	}

	got := toSpawnerRunSpec(run, node)

	if len(got.Command) < 6 {
		t.Fatalf("runtime-helper command length = %d, want >= 6", len(got.Command))
	}
	if got.Command[0] != "/bin/sh" || got.Command[1] != "-ceu" {
		t.Fatalf("runtime-helper command prefix = %q, want [/bin/sh -ceu]", got.Command[:2])
	}
	if !strings.Contains(got.Command[2], "node-contract.json") {
		t.Fatalf("runtime-helper wrapper missing contract path: %q", got.Command[2])
	}
	if !strings.Contains(got.Command[2], ArtifactHelperPath+"\" run --contract") && !strings.Contains(got.Command[2], ArtifactHelperPath+" run --contract") {
		t.Fatalf("runtime-helper wrapper missing nan contract invocation: %q", got.Command[2])
	}
	if got.Command[4] != "sh" || got.Command[5] != "-c" {
		t.Fatalf("runtime-helper original command = %q, want [sh -c ...]", got.Command[4:6])
	}
	if got.Env["JUMI_NODE_CONTRACT_PATH"] != defaultNodeContractPath {
		t.Fatalf("JUMI_NODE_CONTRACT_PATH = %q, want %q", got.Env["JUMI_NODE_CONTRACT_PATH"], defaultNodeContractPath)
	}
	for _, key := range []string{
		"JUMI_INPUT_DATASET_URI",
		"JUMI_INPUT_DATASET_EXPECTED_DIGEST",
		"JUMI_INPUT_DATASET_EXPECTED_SIZE_BYTES",
		"JUMI_INPUT_DATASET_MATERIALIZATION_MODE",
		"JUMI_INPUT_DATASET_LOCAL_PATH",
	} {
		if _, ok := got.Env[key]; ok {
			t.Fatalf("%s should be omitted in runtime-helper contract mode", key)
		}
	}
	raw := got.Env["JUMI_NODE_CONTRACT_JSON"]
	if raw == "" {
		t.Fatal("JUMI_NODE_CONTRACT_JSON = empty, want serialized contract")
	}
	var contract struct {
		SchemaVersion string `json:"schemaVersion"`
		RunID         string `json:"runId"`
		SampleRunID   string `json:"sampleRunId"`
		NodeID        string `json:"nodeId"`
		Paths         struct {
			InputRoot    string `json:"inputRoot"`
			WorkRoot     string `json:"workRoot"`
			OutputRoot   string `json:"outputRoot"`
			ManifestPath string `json:"manifestPath"`
		} `json:"paths"`
		Outputs []struct {
			Name string `json:"name"`
			Path string `json:"path"`
		} `json:"outputs"`
		Inputs []struct {
			Name                string `json:"name"`
			URI                 string `json:"uri"`
			ExpectedDigest      string `json:"expectedDigest"`
			ExpectedSizeBytes   int64  `json:"expectedSizeBytes"`
			MaterializationMode string `json:"materializationMode"`
			LocalPath           string `json:"localPath"`
		} `json:"inputs"`
	}
	if err := json.Unmarshal([]byte(raw), &contract); err != nil {
		t.Fatalf("unmarshal contract json: %v", err)
	}
	if contract.SchemaVersion != nodeContractSchemaVersion {
		t.Fatalf("schemaVersion = %q, want %q", contract.SchemaVersion, nodeContractSchemaVersion)
	}
	if contract.RunID != "run-4" || contract.SampleRunID != "sample-4" || contract.NodeID != "worker" {
		t.Fatalf("unexpected contract identity: %+v", contract)
	}
	if contract.Paths.InputRoot != "/work/inputs" || contract.Paths.WorkRoot != "/work" || contract.Paths.OutputRoot != "/out" {
		t.Fatalf("unexpected contract paths: %+v", contract.Paths)
	}
	if len(contract.Outputs) != 1 || contract.Outputs[0].Name != "report" || contract.Outputs[0].Path != "report" {
		t.Fatalf("unexpected contract outputs: %+v", contract.Outputs)
	}
	if len(contract.Inputs) != 1 {
		t.Fatalf("unexpected contract inputs: %+v", contract.Inputs)
	}
	if got := contract.Inputs[0]; got.Name != "dataset" || got.URI != "http://artifact.local/dataset" || got.ExpectedDigest != "sha256:abc" || got.ExpectedSizeBytes != 17 || got.MaterializationMode != "remote_fetch" || got.LocalPath != "inputs/result" {
		t.Fatalf("unexpected contract input: %+v", got)
	}
}

func TestToSpawnerRunSpecPreservesAttemptAwareManifestPath(t *testing.T) {
	run := spec.RunRecord{RunID: "run-5", Spec: spec.ExecutableRunSpec{Run: spec.RunMetadata{SampleRunID: "sample-5"}}}
	node := spec.Node{
		NodeID:  "worker",
		Image:   "helper-image:latest",
		Command: []string{"echo", "hi"},
		Outputs: []string{"report"},
		Env: map[string]string{
			"JUMI_ATTEMPT_ID":           "run-5-worker-attempt-1",
			"JUMI_OUTPUT_MANIFEST_PATH": "/out/_meta/jumi/runs/run-5/nodes/worker/attempts/run-5-worker-attempt-1/artifacts.manifest.json",
		},
	}

	got := toSpawnerRunSpec(run, node)

	if got.Env["JUMI_ATTEMPT_ID"] != "run-5-worker-attempt-1" {
		t.Fatalf("JUMI_ATTEMPT_ID = %q, want run-5-worker-attempt-1", got.Env["JUMI_ATTEMPT_ID"])
	}
	if got.Env["JUMI_OUTPUT_MANIFEST_PATH"] != "/out/_meta/jumi/runs/run-5/nodes/worker/attempts/run-5-worker-attempt-1/artifacts.manifest.json" {
		t.Fatalf("JUMI_OUTPUT_MANIFEST_PATH = %q, want attempt-aware path", got.Env["JUMI_OUTPUT_MANIFEST_PATH"])
	}
}

func TestValidateObservedManifestAllowsSupportedSchemaVersions(t *testing.T) {
	node := spec.Node{Env: map[string]string{"JUMI_RUN_ID": "run-1", "JUMI_NODE_ID": "node-1", "JUMI_ATTEMPT_ID": "attempt-1"}}
	cases := []string{"", provenance.ArtifactManifestSchemaVersion, "nan.artifactManifest.v1"}
	for _, schemaVersion := range cases {
		manifest := provenance.ArtifactManifest{
			SchemaVersion: schemaVersion,
			RunID:         "run-1",
			NodeID:        "node-1",
			AttemptID:     "attempt-1",
		}
		if err := validateObservedManifest(manifest, node); err != nil {
			t.Fatalf("schemaVersion %q rejected: %v", schemaVersion, err)
		}
	}
}

func TestValidateObservedManifestRejectsUnknownSchemaVersion(t *testing.T) {
	node := spec.Node{Env: map[string]string{"JUMI_RUN_ID": "run-1", "JUMI_NODE_ID": "node-1", "JUMI_ATTEMPT_ID": "attempt-1"}}
	manifest := provenance.ArtifactManifest{
		SchemaVersion: "unknown.manifest.v1",
		RunID:         "run-1",
		NodeID:        "node-1",
		AttemptID:     "attempt-1",
	}
	if err := validateObservedManifest(manifest, node); err == nil {
		t.Fatal("validateObservedManifest() error = nil, want unsupported schema version")
	}
}

func TestBuildDirectK8sJobUsesServiceAccountAndWorkingDir(t *testing.T) {
	job := buildDirectK8sJob(spapi.RunSpec{
		RunID:    "run-1-produce",
		ImageRef: "busybox:1.36",
		Command:  []string{"sh", "-c", "echo hi"},
		Env:      map[string]string{"A": "B"},
		Labels: map[string]string{
			"kueue.x-k8s.io/queue-name": "standard",
		},
		Annotations: map[string]string{"anno": "value"},
		Mounts: []spapi.Mount{
			{Source: "work", Target: "/work", ReadOnly: false},
		},
		Resources: spapi.Resources{CPU: "250m", Memory: "128Mi"},
		Cleanup:   spapi.CleanupPolicy{TTLSecondsAfterFinished: 600},
		Placement: &spapi.Placement{NodeSelector: map[string]string{"kubernetes.io/hostname": "lab-worker-1"}},
	}, "jumi-ah-dev", "/workspace", "jumi")

	if got := job.Spec.Template.Spec.ServiceAccountName; got != "jumi" {
		t.Fatalf("serviceAccountName = %q, want jumi", got)
	}
	if got := job.Spec.Template.Spec.Containers[0].WorkingDir; got != "/workspace" {
		t.Fatalf("workingDir = %q, want /workspace", got)
	}
}

func TestShouldUseDirectK8sStartWhenOptionalFieldsRequested(t *testing.T) {
	if !shouldUseDirectK8sStart(preparedSpawnerNode{serviceAccountName: "jumi"}) {
		t.Fatal("expected direct start when serviceAccountName is set")
	}
	if !shouldUseDirectK8sStart(preparedSpawnerNode{workingDir: "/workspace"}) {
		t.Fatal("expected direct start when workingDir is set")
	}
	if !shouldUseDirectK8sStart(preparedSpawnerNode{runSpec: spapi.RunSpec{Mounts: []spapi.Mount{{Source: "hostpath:/var/lib/jumi-artifacts", Target: "/var/lib/jumi-artifacts"}}}}) {
		t.Fatal("expected direct start when hostpath mount is requested")
	}
	if shouldUseDirectK8sStart(preparedSpawnerNode{}) {
		t.Fatal("did not expect direct start without optional fields")
	}
}

func TestBuildDirectVolumesSupportsHostPathMounts(t *testing.T) {
	volumes, mounts := buildDirectVolumes([]spapi.Mount{{
		Source:   "hostpath:/var/lib/jumi-artifacts",
		Target:   "/var/lib/jumi-artifacts",
		ReadOnly: true,
	}})
	if len(volumes) != 1 || len(mounts) != 1 {
		t.Fatalf("volumes=%d mounts=%d, want 1 each", len(volumes), len(mounts))
	}
	if volumes[0].HostPath == nil {
		t.Fatalf("volume source = %#v, want hostPath", volumes[0].VolumeSource)
	}
	if volumes[0].HostPath.Path != "/var/lib/jumi-artifacts" {
		t.Fatalf("hostPath.path = %q, want /var/lib/jumi-artifacts", volumes[0].HostPath.Path)
	}
	if volumes[0].HostPath.Type == nil || *volumes[0].HostPath.Type != corev1.HostPathDirectoryOrCreate {
		t.Fatalf("hostPath.type = %#v, want DirectoryOrCreate", volumes[0].HostPath.Type)
	}
	if mounts[0].MountPath != "/var/lib/jumi-artifacts" || !mounts[0].ReadOnly {
		t.Fatalf("mount = %#v, want RO mount at node-local path", mounts[0])
	}
}

func TestReadArtifactsManifestFromTerminationMessage(t *testing.T) {
	pod := &corev1.Pod{
		Status: corev1.PodStatus{
			ContainerStatuses: []corev1.ContainerStatus{
				{
					Name: "main",
					State: corev1.ContainerState{
						Terminated: &corev1.ContainerStateTerminated{
							Message: "{\"artifacts\":[{\"outputName\":\"report\",\"uri\":\"jumi://runs/run-1/nodes/produce/outputs/report\",\"digest\":\"sha256:abc\",\"sizeBytes\":10}]}",
						},
					},
				},
			},
		},
	}

	raw, err := readArtifactsManifestFromTerminationMessage(pod)
	if err != nil {
		t.Fatalf("readArtifactsManifestFromTerminationMessage() error = %v", err)
	}
	if string(raw) == "" {
		t.Fatal("termination message manifest = empty, want payload")
	}
	if !strings.Contains(string(raw), "\"outputName\":\"report\"") {
		t.Fatalf("termination message manifest = %q, want report payload", string(raw))
	}
}

func TestDirectSanitizeNameNoDanglingDash(t *testing.T) {
	// Build an id whose sanitized form ends in '-' at exactly 63 chars.
	// e.g. "aaa...a_" → lowercased chars + one non-alphanum at position 63.
	id := strings.Repeat("a", 62) + "_b" // after sanitize: 63 'a's + '-' + 'b' = 65 chars
	got := directSanitizeName(id)
	if len(got) > 63 {
		t.Fatalf("name too long: %d chars", len(got))
	}
	if strings.HasSuffix(got, "-") {
		t.Fatalf("name ends with dash: %q", got)
	}
}

func optionalStringField(v any, fieldName string) (string, bool) {
	rv := reflect.ValueOf(v)
	field := rv.FieldByName(fieldName)
	if !field.IsValid() || field.Kind() != reflect.String {
		return "", false
	}
	return field.String(), true
}
