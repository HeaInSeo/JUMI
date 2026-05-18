package provenance

import "testing"

func TestParseArtifactManifest(t *testing.T) {
	manifest, err := ParseArtifactManifest([]byte(`{
		"schemaVersion": "jumi.observedArtifactManifest.v1",
		"runId": "r1",
		"nodeId": "a",
		"attemptId": "r1-a-attempt-1",
		"artifacts": [
			{"outputName":"result.json","declaredPath":"result.json","absolutePath":"/out/result.json","type":"file","uri":"jumi://runs/r1/nodes/a/outputs/result.json","digest":"sha256:abc","sizeBytes":2048}
		]
	}`))
	if err != nil {
		t.Fatalf("ParseArtifactManifest() error = %v", err)
	}
	if manifest.SchemaVersion != ArtifactManifestSchemaVersion {
		t.Fatalf("manifest.SchemaVersion = %q, want %q", manifest.SchemaVersion, ArtifactManifestSchemaVersion)
	}
	if manifest.AttemptID != "r1-a-attempt-1" {
		t.Fatalf("manifest.AttemptID = %q, want r1-a-attempt-1", manifest.AttemptID)
	}
	record, ok := manifest.ByOutputName("result.json")
	if !ok {
		t.Fatal("ByOutputName(result.json) = false, want true")
	}
	if record.SizeBytes != 2048 {
		t.Fatalf("record.SizeBytes = %d, want 2048", record.SizeBytes)
	}
	if record.AbsolutePath != "/out/result.json" {
		t.Fatalf("record.AbsolutePath = %q, want /out/result.json", record.AbsolutePath)
	}
}

func TestParseArtifactManifestRejectsDuplicateOutputName(t *testing.T) {
	_, err := ParseArtifactManifest([]byte(`{
		"artifacts": [
			{"outputName":"result.json"},
			{"outputName":"result.json"}
		]
	}`))
	if err == nil {
		t.Fatal("ParseArtifactManifest() error = nil, want duplicate outputName error")
	}
}

func TestAttemptArtifactsManifestPath(t *testing.T) {
	got := AttemptArtifactsManifestPath("run 1", "node/a", "attempt:1")
	want := "/out/_meta/jumi/runs/run%201/nodes/node%2Fa/attempts/attempt:1/artifacts.manifest.json"
	if got != want {
		t.Fatalf("AttemptArtifactsManifestPath() = %q, want %q", got, want)
	}
}
