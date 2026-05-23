package provenance

import "testing"

func TestParseArtifactManifest(t *testing.T) {
	manifest, err := ParseArtifactManifest([]byte(`{
		"schemaVersion": "jumi.observedArtifactManifest.v1",
		"runId": "r1",
		"sampleRunId": "sample-1",
		"nodeId": "a",
		"attemptId": "r1-a-attempt-1",
		"outputRoot": "/out",
		"artifacts": [
			{
				"outputName":"result.json",
				"declaredPath":"result.json",
				"absolutePath":"/out/result.json",
				"type":"file",
				"uri":"jumi://runs/r1/nodes/a/outputs/result.json",
				"logicalUri":"jumi://runs/r1/nodes/a/outputs/result",
				"digest":"sha256:abc",
				"sizeBytes":2048,
				"producerAttemptId":"r1-a-attempt-1",
				"locations":[
					{
						"nodeLocal":{
							"nodeName":"worker-2",
							"path":"/var/lib/jumi-artifacts/cas/sha256/abc"
						}
					}
				],
				"provenance":{
					"inputs":[
						{
							"inputName":"dataset",
							"artifactDigest":"sha256:def",
							"producerLogicalUri":"jumi://runs/r1/nodes/z/outputs/dataset"
						}
					]
				}
			}
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
	if manifest.OutputRoot != "/out" {
		t.Fatalf("manifest.OutputRoot = %q, want /out", manifest.OutputRoot)
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
	if record.LogicalURI != "jumi://runs/r1/nodes/a/outputs/result" {
		t.Fatalf("record.LogicalURI = %q, want logical URI", record.LogicalURI)
	}
	if record.ProducerAttemptID != "r1-a-attempt-1" {
		t.Fatalf("record.ProducerAttemptID = %q, want r1-a-attempt-1", record.ProducerAttemptID)
	}
	if len(record.Locations) != 1 || record.Locations[0].NodeLocal == nil {
		t.Fatalf("record.Locations = %#v, want one nodeLocal location", record.Locations)
	}
	if record.Locations[0].NodeLocal.NodeName != "worker-2" {
		t.Fatalf("record.Locations[0].NodeLocal.NodeName = %q, want worker-2", record.Locations[0].NodeLocal.NodeName)
	}
	if record.Locations[0].NodeLocal.Path != "/var/lib/jumi-artifacts/cas/sha256/abc" {
		t.Fatalf("record.Locations[0].NodeLocal.Path = %q, want CAS path", record.Locations[0].NodeLocal.Path)
	}
	if record.Provenance == nil || len(record.Provenance.Inputs) != 1 {
		t.Fatalf("record.Provenance = %#v, want one input lineage", record.Provenance)
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
