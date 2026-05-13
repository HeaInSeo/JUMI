package handoff

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestHTTPClientResolveBinding(t *testing.T) {
	client := NewHTTPClientWithClient("http://artifact-handoff.test", &http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			switch r.URL.Path {
			case "/v1/handoffs:resolve":
				return jsonResponse(http.StatusOK, `{"resolutionStatus":"RESOLVED","decision":"remote_fetch","sourceNodeName":"node-a","artifactURI":"http://artifact.local/a","requiresMaterialization":true}`), nil
			case "/v1/nodes:notifyTerminal", "/v1/sampleRuns:finalize", "/v1/sampleRuns:evaluateGC":
				return jsonResponse(http.StatusOK, `{"accepted":true}`), nil
			default:
				t.Fatalf("unexpected path: %s", r.URL.Path)
				return nil, nil
			}
		}),
	})
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

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return fn(r)
}

func jsonResponse(statusCode int, body string) *http.Response {
	return &http.Response{
		StatusCode: statusCode,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}
