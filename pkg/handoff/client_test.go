package handoff

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestHTTPClientResolveBinding(t *testing.T) {
	var resolvePayload map[string]any
	var notifyPayload map[string]any
	var registerPayload map[string]any
	client := NewHTTPClientWithClient("http://artifact-handoff.test", &http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Fatalf("read request body: %v", err)
			}
			switch r.URL.Path {
			case "/v1/handoffs:resolve":
				if err := json.Unmarshal(body, &resolvePayload); err != nil {
					t.Fatalf("unmarshal resolve payload: %v", err)
				}
				return jsonResponse(http.StatusOK, `{"resolutionStatus":"RESOLVED","decision":"remote_fetch","placementIntent":{"mode":"required_node","nodeName":"node-a"},"materializationPlan":{"mode":"local_reuse","uri":"http://artifact.local/a","expectedDigest":"sha256:abc","sourceLocation":{"nodeLocal":{"nodeName":"node-a","path":"/jumi-node-artifacts/cas/sha256/abc"}},"localPath":"/work/inputs/dataset"},"reason":"remote fetch required","retryable":false}`), nil
			case "/v1/artifacts:register":
				if err := json.Unmarshal(body, &registerPayload); err != nil {
					t.Fatalf("unmarshal register payload: %v", err)
				}
				return jsonResponse(http.StatusOK, `{"availabilityState":"LOCAL_ONLY"}`), nil
			case "/v1/nodes:notifyTerminal", "/v1/sampleRuns:finalize", "/v1/sampleRuns:evaluateGC":
				if r.URL.Path == "/v1/nodes:notifyTerminal" {
					if err := json.Unmarshal(body, &notifyPayload); err != nil {
						t.Fatalf("unmarshal notify payload: %v", err)
					}
				}
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
	if resp.PlacementIntent.NodeName != "node-a" {
		t.Fatalf("placement node = %q, want node-a", resp.PlacementIntent.NodeName)
	}
	if resp.MaterializationPlan.URI != "http://artifact.local/a" {
		t.Fatalf("artifact URI = %q, want http://artifact.local/a", resp.MaterializationPlan.URI)
	}
	if resp.MaterializationPlan.Mode != "local_reuse" {
		t.Fatalf("materialization mode = %q, want local_reuse", resp.MaterializationPlan.Mode)
	}
	if resp.MaterializationPlan.SourceLocation == nil || resp.MaterializationPlan.SourceLocation.NodeLocal == nil {
		t.Fatalf("sourceLocation = %#v, want nodeLocal source", resp.MaterializationPlan.SourceLocation)
	}
	if resp.MaterializationPlan.SourceLocation.NodeLocal.Path != "/jumi-node-artifacts/cas/sha256/abc" {
		t.Fatalf("sourceLocation.nodeLocal.path = %q, want node-local path", resp.MaterializationPlan.SourceLocation.NodeLocal.Path)
	}
	if resp.MaterializationPlan.LocalPath != "/work/inputs/dataset" {
		t.Fatalf("localPath = %q, want /work/inputs/dataset", resp.MaterializationPlan.LocalPath)
	}
	binding, ok := resolvePayload["binding"].(map[string]any)
	if !ok {
		t.Fatalf("resolve payload binding = %#v, want object", resolvePayload["binding"])
	}
	if binding["producerNodeId"] != "parent-a" {
		t.Fatalf("resolve payload producerNodeId = %#v, want parent-a", binding["producerNodeId"])
	}
	if binding["consumePolicy"] != "RemoteOK" {
		t.Fatalf("resolve payload consumePolicy = %#v, want RemoteOK", binding["consumePolicy"])
	}
	if binding["required"] != true {
		t.Fatalf("resolve payload required = %#v, want true", binding["required"])
	}
	if err := client.RegisterArtifact(context.Background(), RegisterArtifactRequest{
		SampleRunID:       "sample-1",
		ProducerNodeID:    "parent-a",
		ProducerAttemptID: "attempt-1",
		OutputName:        "output",
		ArtifactID:        "sample-1/parent-a/attempt-1/output",
		Digest:            "sha256:abc",
		URI:               "jumi://runs/run-1/nodes/parent-a/outputs/output",
		LogicalURI:        "jumi://runs/run-1/nodes/parent-a/outputs/output",
		Locations: []ArtifactLocation{{
			NodeLocal: &NodeLocalLocation{NodeName: "node-a", Path: "/var/lib/jumi-artifacts/cas/sha256/abc"},
		}},
		SizeBytes: 42,
	}); err != nil {
		t.Fatalf("RegisterArtifact() error = %v", err)
	}
	artifact, ok := registerPayload["artifact"].(map[string]any)
	if !ok {
		t.Fatalf("register payload artifact = %#v, want object", registerPayload["artifact"])
	}
	if artifact["producerAttemptId"] != "attempt-1" {
		t.Fatalf("register payload producerAttemptId = %#v, want attempt-1", artifact["producerAttemptId"])
	}
	if artifact["logicalUri"] != "jumi://runs/run-1/nodes/parent-a/outputs/output" {
		t.Fatalf("register payload logicalUri = %#v, want logical URI", artifact["logicalUri"])
	}
	if err := client.NotifyNodeTerminal(context.Background(), NotifyNodeTerminalRequest{
		SampleRunID:   "sample-1",
		NodeID:        "child-a",
		AttemptID:     "attempt-1",
		TerminalState: "Succeeded",
	}); err != nil {
		t.Fatalf("NotifyNodeTerminal() error = %v", err)
	}
	if notifyPayload["attemptId"] != "attempt-1" {
		t.Fatalf("notify payload attemptId = %#v, want attempt-1", notifyPayload["attemptId"])
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

func TestHTTPClientResolveBindingDecodesLegacyResponseShape(t *testing.T) {
	client := NewHTTPClientWithClient("http://artifact-handoff.test", &http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			if r.URL.Path != "/v1/handoffs:resolve" {
				t.Fatalf("unexpected path: %s", r.URL.Path)
			}
			return jsonResponse(http.StatusOK, `{"resolutionStatus":"RESOLVED","decision":"remote_fetch","sourceNodeName":"node-a","artifactURI":"http://artifact.local/output","requiresMaterialization":true}`), nil
		}),
	})
	resp, err := client.ResolveBinding(context.Background(), ResolveBindingRequest{
		SampleRunID:        "sample-legacy",
		ChildNodeID:        "child-a",
		BindingName:        "dataset",
		ProducerNodeID:     "parent-a",
		ProducerOutputName: "output",
	})
	if err != nil {
		t.Fatalf("ResolveBinding() error = %v", err)
	}
	if resp.PlacementIntent.NodeName != "node-a" {
		t.Fatalf("placement node = %q, want node-a", resp.PlacementIntent.NodeName)
	}
	if resp.MaterializationPlan.URI != "http://artifact.local/output" {
		t.Fatalf("artifact URI = %q, want http://artifact.local/output", resp.MaterializationPlan.URI)
	}
	if resp.MaterializationPlan.Mode != "remote_fetch" {
		t.Fatalf("materialization mode = %q, want remote_fetch", resp.MaterializationPlan.Mode)
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
