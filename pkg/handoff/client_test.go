package handoff

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHTTPClientResolveBinding(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/handoffs:resolve":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"resolutionStatus":"RESOLVED","decision":"remote_fetch","sourceNodeName":"node-a","artifactURI":"http://artifact.local/a","requiresMaterialization":true}`))
		case "/v1/nodes:notifyTerminal", "/v1/sampleRuns:finalize", "/v1/sampleRuns:evaluateGC":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"accepted":true}`))
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	client := NewHTTPClient(server.URL, 0)
	resp, err := client.ResolveBinding(context.Background(), ResolveBindingRequest{
		SampleRunID:        "sample-1",
		ChildNodeID:        "child-a",
		BindingName:        "dataset",
		ProducerNodeID:     "parent-a",
		ProducerOutputName: "output",
		ConsumePolicy:      "RemoteOK",
		Required:           true,
	})
	if err != nil {
		t.Fatalf("ResolveBinding() error = %v", err)
	}
	if resp.ResolutionStatus != "RESOLVED" {
		t.Fatalf("resolution status = %q, want RESOLVED", resp.ResolutionStatus)
	}
	if resp.Decision != "remote_fetch" {
		t.Fatalf("decision = %q, want remote_fetch", resp.Decision)
	}
	if resp.SourceNodeName != "node-a" {
		t.Fatalf("source node = %q, want node-a", resp.SourceNodeName)
	}
	if resp.ArtifactURI != "http://artifact.local/a" {
		t.Fatalf("artifact URI = %q, want http://artifact.local/a", resp.ArtifactURI)
	}
	if !resp.RequiresMaterialization {
		t.Fatal("expected requiresMaterialization to be true")
	}
	if err := client.NotifyNodeTerminal(context.Background(), NotifyNodeTerminalRequest{
		SampleRunID:   "sample-1",
		NodeID:        "child-a",
		TerminalState: "Succeeded",
	}); err != nil {
		t.Fatalf("NotifyNodeTerminal() error = %v", err)
	}
	if err := client.FinalizeSampleRun(context.Background(), FinalizeSampleRunRequest{
		SampleRunID: "sample-1",
	}); err != nil {
		t.Fatalf("FinalizeSampleRun() error = %v", err)
	}
	if err := client.EvaluateGC(context.Background(), EvaluateGCRequest{
		SampleRunID: "sample-1",
	}); err != nil {
		t.Fatalf("EvaluateGC() error = %v", err)
	}
}
