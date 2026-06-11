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
