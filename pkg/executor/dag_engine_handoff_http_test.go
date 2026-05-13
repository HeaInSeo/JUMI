package executor

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/HeaInSeo/JUMI/pkg/handoff"
	"github.com/HeaInSeo/JUMI/pkg/registry"
	"github.com/HeaInSeo/JUMI/pkg/spec"
)

func TestDagEngineResolvesBindingsThroughHTTPClient(t *testing.T) {
	reg := registry.NewMemoryRegistry()
	adapter := &fakeAdapter{failOn: map[string]bool{}}

	client := handoff.NewHTTPClientWithClient("http://artifact-handoff.test", &http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			if r.Method != http.MethodPost {
				return jsonResponse(http.StatusNotFound, `{"error":"not found"}`), nil
			}
			switch r.URL.Path {
			case "/v1/artifacts:register":
				return jsonResponse(http.StatusOK, `{"availabilityState":"LOCAL_ONLY"}`), nil
			case "/v1/handoffs:resolve":
				return jsonResponse(http.StatusOK, `{"resolutionStatus":"RESOLVED","decision":"remote_fetch","sourceNodeName":"node-a","artifactURI":"http://artifact.local/output","requiresMaterialization":true}`), nil
			case "/v1/nodes:notifyTerminal", "/v1/sampleRuns:finalize", "/v1/sampleRuns:evaluateGC":
				return jsonResponse(http.StatusOK, `{"accepted":true}`), nil
			default:
				return jsonResponse(http.StatusNotFound, `{"error":"not found"}`), nil
			}
		}),
	})

	engine := NewDagEngineWithHandoff(reg, adapter, client)
	specInput := spec.ExecutableRunSpec{
		Run: spec.RunMetadata{RunID: "run-http", SampleRunID: "sample-http", SubmittedAt: time.Now().UTC(), FailurePolicy: spec.FailurePolicy{Mode: "fail-fast"}},
		Graph: spec.Graph{
			Nodes: []spec.Node{
				{NodeID: "a", Image: "busybox:1.36", Outputs: []string{"output"}},
				{NodeID: "b", Image: "busybox:1.36", ArtifactBindings: []spec.ArtifactBinding{{
					BindingName:        "dataset",
					ChildInputName:     "dataset",
					ProducerNodeID:     "a",
					ProducerOutputName: "output",
					ConsumePolicy:      "RemoteOK",
					Required:           true,
				}}},
			},
			Edges: [][]string{{"a", "b"}},
		},
	}
	record := spec.RunRecord{RunID: specInput.Run.RunID, Status: spec.RunStatusAccepted, AcceptedAt: time.Now().UTC(), Spec: specInput}
	nodes := []spec.NodeRecord{{RunID: record.RunID, NodeID: "a", Status: spec.NodeStatusPending}, {RunID: record.RunID, NodeID: "b", Status: spec.NodeStatusPending}}
	if err := reg.CreateRun(context.Background(), record, nodes); err != nil {
		t.Fatalf("CreateRun() error = %v", err)
	}
	if err := engine.Admit(context.Background(), record); err != nil {
		t.Fatalf("Admit() error = %v", err)
	}
	waitForRunStatus(t, reg, record.RunID, spec.RunStatusSucceeded)
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
