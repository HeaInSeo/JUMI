package backend

import (
	"strings"
	"testing"

	"github.com/HeaInSeo/JUMI/pkg/provenance"
	"github.com/HeaInSeo/JUMI/pkg/spec"
	corev1 "k8s.io/api/core/v1"
)

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

func TestToAttemptRequestDoesNotExposeLegacyJumiLabels(t *testing.T) {
	run := spec.RunRecord{
		RunID: "run-1",
		Spec: spec.ExecutableRunSpec{
			Run: spec.RunMetadata{TraceID: "trace-1"},
		},
	}
	node := spec.Node{
		NodeID: "node-1",
		Image:  "tool:latest",
		Env: map[string]string{
			"JUMI_RUN_ID":     "run-1",
			"JUMI_NODE_ID":    "node-1",
			"JUMI_ATTEMPT_ID": "attempt-1",
		},
		Kueue: &spec.KueueHints{
			QueueName: "gpu-batch",
			Labels: map[string]string{
				"user.jumi.io/team": "genomics",
			},
		},
	}

	req := toAttemptRequest(run, node)
	if _, ok := req.UserLabels["jumi/run-id"]; ok {
		t.Fatal("legacy jumi/run-id label was propagated")
	}
	if _, ok := req.UserLabels["jumi/node-id"]; ok {
		t.Fatal("legacy jumi/node-id label was propagated")
	}
	if got := req.UserLabels["kueue.x-k8s.io/queue-name"]; got != "gpu-batch" {
		t.Fatalf("kueue queue label = %q, want gpu-batch", got)
	}
	if got := req.UserLabels["user.jumi.io/team"]; got != "genomics" {
		t.Fatalf("user label = %q, want genomics", got)
	}
	if got := req.UserAnnotations["jumi.trace-id"]; got != "trace-1" {
		t.Fatalf("trace annotation = %q, want trace-1", got)
	}
}
