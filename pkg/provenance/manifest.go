package provenance

import (
	"encoding/json"
	"fmt"
	"net/url"
)

const DefaultArtifactsManifestPath = "/out/_meta/artifacts.manifest.json"
const ArtifactManifestSchemaVersion = "jumi.observedArtifactManifest.v1"

type ArtifactManifest struct {
	SchemaVersion string           `json:"schemaVersion,omitempty"`
	RunID         string           `json:"runId,omitempty"`
	SampleRunID   string           `json:"sampleRunId,omitempty"`
	NodeID        string           `json:"nodeId,omitempty"`
	AttemptID     string           `json:"attemptId,omitempty"`
	ContainerName string           `json:"containerName,omitempty"`
	HelperVersion string           `json:"helperVersion,omitempty"`
	CreatedAt     string           `json:"createdAt,omitempty"`
	OutputRoot    string           `json:"outputRoot,omitempty"`
	Artifacts     []ArtifactRecord `json:"artifacts"`
}

type ArtifactRecord struct {
	OutputName        string             `json:"outputName"`
	DeclaredPath      string             `json:"declaredPath,omitempty"`
	AbsolutePath      string             `json:"absolutePath,omitempty"`
	Type              string             `json:"type,omitempty"`
	URI               string             `json:"uri,omitempty"`
	LogicalURI        string             `json:"logicalUri,omitempty"`
	Digest            string             `json:"digest,omitempty"`
	SizeBytes         int64              `json:"sizeBytes,omitempty"`
	ProducerAttemptID string             `json:"producerAttemptId,omitempty"`
	Locations         []ArtifactLocation `json:"locations,omitempty"`
	Provenance        *ArtifactLineage   `json:"provenance,omitempty"`
}

type ArtifactLocation struct {
	NodeLocal *NodeLocalLocation `json:"nodeLocal,omitempty"`
}

type NodeLocalLocation struct {
	NodeName string `json:"nodeName,omitempty"`
	Path     string `json:"path,omitempty"`
}

type ArtifactLineage struct {
	Inputs []ArtifactLineageInput `json:"inputs,omitempty"`
}

type ArtifactLineageInput struct {
	InputName          string `json:"inputName,omitempty"`
	ArtifactDigest     string `json:"artifactDigest,omitempty"`
	ProducerLogicalURI string `json:"producerLogicalUri,omitempty"`
}

func ParseArtifactManifest(data []byte) (ArtifactManifest, error) {
	var manifest ArtifactManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return ArtifactManifest{}, err
	}
	seen := make(map[string]struct{}, len(manifest.Artifacts))
	for _, artifact := range manifest.Artifacts {
		if artifact.OutputName == "" {
			return ArtifactManifest{}, fmt.Errorf("artifact outputName is required")
		}
		if _, ok := seen[artifact.OutputName]; ok {
			return ArtifactManifest{}, fmt.Errorf("duplicate artifact outputName %q", artifact.OutputName)
		}
		seen[artifact.OutputName] = struct{}{}
	}
	return manifest, nil
}

func (m ArtifactManifest) ByOutputName(name string) (ArtifactRecord, bool) {
	for _, artifact := range m.Artifacts {
		if artifact.OutputName == name {
			return artifact, true
		}
	}
	return ArtifactRecord{}, false
}

func AttemptArtifactsManifestPath(runID, nodeID, attemptID string) string {
	return fmt.Sprintf(
		"/out/_meta/jumi/runs/%s/nodes/%s/attempts/%s/artifacts.manifest.json",
		url.PathEscape(runID),
		url.PathEscape(nodeID),
		url.PathEscape(attemptID),
	)
}
