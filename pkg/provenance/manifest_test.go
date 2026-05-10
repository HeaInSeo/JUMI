package provenance

import "testing"

func TestParseArtifactManifest(t *testing.T) {
	manifest, err := ParseArtifactManifest([]byte(`{
		"artifacts": [
			{"outputName":"result.json","uri":"jumi://runs/r1/nodes/a/outputs/result.json","digest":"sha256:abc","sizeBytes":2048}
		]
	}`))
	if err != nil {
		t.Fatalf("ParseArtifactManifest() error = %v", err)
	}
	record, ok := manifest.ByOutputName("result.json")
	if !ok {
		t.Fatal("ByOutputName(result.json) = false, want true")
	}
	if record.SizeBytes != 2048 {
		t.Fatalf("record.SizeBytes = %d, want 2048", record.SizeBytes)
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
