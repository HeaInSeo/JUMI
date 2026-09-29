package handoff

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"sync"
	"testing"

	ahv1 "github.com/HeaInSeo/JUMI/pkg/handoff/ahv1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
)

// runIDRecorder records the run_id of every artifact-handoff RPC it receives.
type runIDRecorder struct {
	ahv1.UnimplementedArtifactHandoffResolverServer
	mu     sync.Mutex
	runIDs map[string]string
	calls  int
}

func (s *runIDRecorder) record(op, runID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.runIDs == nil {
		s.runIDs = map[string]string{}
	}
	s.runIDs[op] = runID
	s.calls++
}

func (s *runIDRecorder) ResolveHandoff(_ context.Context, req *ahv1.ResolveHandoffRequest) (*ahv1.ResolveHandoffResponse, error) {
	s.record("resolve", req.GetBinding().GetRunId())
	return &ahv1.ResolveHandoffResponse{ResolutionStatus: "RESOLVED"}, nil
}

func (s *runIDRecorder) RegisterArtifact(_ context.Context, req *ahv1.RegisterArtifactRequest) (*ahv1.RegisterArtifactResponse, error) {
	s.record("register", req.GetArtifact().GetRunId())
	return &ahv1.RegisterArtifactResponse{}, nil
}

func (s *runIDRecorder) NotifyNodeTerminal(_ context.Context, req *ahv1.NotifyNodeTerminalRequest) (*ahv1.NotifyNodeTerminalResponse, error) {
	s.record("notify", req.GetRunId())
	return &ahv1.NotifyNodeTerminalResponse{Accepted: true}, nil
}

func (s *runIDRecorder) FinalizeSampleRun(_ context.Context, req *ahv1.FinalizeSampleRunRequest) (*ahv1.FinalizeSampleRunResponse, error) {
	s.record("finalize", req.GetRunId())
	return &ahv1.FinalizeSampleRunResponse{Accepted: true}, nil
}

func (s *runIDRecorder) EvaluateGC(_ context.Context, req *ahv1.EvaluateGCRequest) (*ahv1.EvaluateGCResponse, error) {
	s.record("gc", req.GetRunId())
	return &ahv1.EvaluateGCResponse{Accepted: true}, nil
}

func (s *runIDRecorder) GetSampleRunLifecycle(_ context.Context, req *ahv1.GetSampleRunLifecycleRequest) (*ahv1.GetSampleRunLifecycleResponse, error) {
	s.record("lifecycle", req.GetRunId())
	return &ahv1.GetSampleRunLifecycleResponse{RunId: req.GetRunId()}, nil
}

func newRunIDRecorderClient(t *testing.T) (*GRPCClient, *runIDRecorder) {
	t.Helper()
	listener := bufconn.Listen(1024 * 1024)
	server := grpc.NewServer()
	rec := &runIDRecorder{}
	ahv1.RegisterArtifactHandoffResolverServer(server, rec)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)
	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
			return listener.Dial()
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("grpc.NewClient() error = %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return &GRPCClient{conn: conn, client: ahv1.NewArtifactHandoffResolverClient(conn)}, rec
}

// callAll invokes every artifact-handoff operation with runID and the same
// SampleRunID, and returns the error of each operation.
func callAll(c Client, runID string) map[string]error {
	ctx := context.Background()
	errs := map[string]error{}
	_, errs["resolve"] = c.ResolveBinding(ctx, ResolveBindingRequest{
		RunID: runID, SampleRunID: "sample-1", ChildNodeID: "child", BindingName: "in",
		ProducerNodeID: "producer", ProducerOutputName: "out",
	})
	errs["register"] = c.RegisterArtifact(ctx, RegisterArtifactRequest{
		RunID: runID, SampleRunID: "sample-1", ProducerNodeID: "producer",
		ProducerAttemptID: "a1", OutputName: "out", Digest: "sha256:abc", URI: "http://x",
	})
	errs["notify"] = c.NotifyNodeTerminal(ctx, NotifyNodeTerminalRequest{
		RunID: runID, NodeID: "producer", AttemptID: "a1", TerminalState: "Succeeded",
	})
	errs["finalize"] = c.FinalizeSampleRun(ctx, FinalizeSampleRunRequest{RunID: runID, SampleRunID: "sample-1"})
	errs["gc"] = c.EvaluateGC(ctx, EvaluateGCRequest{RunID: runID})
	_, _, errs["lifecycle"] = c.GetSampleRunLifecycle(ctx, GetSampleRunLifecycleRequest{RunID: runID})
	return errs
}

// Every gRPC call carries the canonical run_id on the wire, and two Runs of
// the same Sample stay distinct.
func TestGRPCClient_SendsRunIDOnEveryCall(t *testing.T) {
	c, rec := newRunIDRecorderClient(t)
	for _, runID := range []string{"run-r1", "run-r2"} {
		for op, err := range callAll(c, runID) {
			if err != nil {
				t.Fatalf("%s(%s) error = %v", op, runID, err)
			}
		}
		for _, op := range []string{"resolve", "register", "notify", "finalize", "gc", "lifecycle"} {
			rec.mu.Lock()
			got := rec.runIDs[op]
			rec.mu.Unlock()
			if got != runID {
				t.Fatalf("%s run_id = %q, want %q (same Sample must not merge Runs)", op, got, runID)
			}
		}
	}
}

// Every HTTP call carries runId in its body (or query for the lifecycle GET),
// with sampleRunId only as metadata where the endpoint accepts it.
func TestHTTPClient_SendsRunIDOnEveryCall(t *testing.T) {
	var mu sync.Mutex
	got := map[string]map[string]any{}
	c := NewHTTPClientWithClient("http://ah.test", &http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			fields := map[string]any{}
			if r.Method == http.MethodGet {
				fields["runId"] = r.URL.Query().Get("runId")
				if r.URL.Query().Has("sampleRunId") {
					t.Errorf("lifecycle query uses sampleRunId: %s", r.URL)
				}
			} else {
				body, err := io.ReadAll(r.Body)
				if err != nil {
					t.Fatalf("read body: %v", err)
				}
				var payload map[string]any
				if err := json.Unmarshal(body, &payload); err != nil {
					t.Fatalf("unmarshal %s: %v", r.URL.Path, err)
				}
				fields = payload
				for _, wrapper := range []string{"binding", "artifact"} {
					if inner, ok := payload[wrapper].(map[string]any); ok {
						fields = inner
					}
				}
			}
			mu.Lock()
			got[r.URL.Path] = fields
			mu.Unlock()
			if r.Method == http.MethodGet {
				return jsonResponse(http.StatusOK, `{"runId":"run-1"}`), nil
			}
			return jsonResponse(http.StatusOK, `{"resolutionStatus":"RESOLVED","accepted":true}`), nil
		}),
	})
	for op, err := range callAll(c, "run-1") {
		if err != nil {
			t.Fatalf("%s error = %v", op, err)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	for _, path := range []string{"/v1/handoffs:resolve", "/v1/artifacts:register", "/v1/nodes:notifyTerminal",
		"/v1/sampleRuns:finalize", "/v1/sampleRuns:evaluateGC", "/v1/sampleRuns:lifecycle"} {
		if got[path]["runId"] != "run-1" {
			t.Fatalf("%s runId = %#v, want run-1", path, got[path]["runId"])
		}
	}
	for _, path := range []string{"/v1/handoffs:resolve", "/v1/artifacts:register", "/v1/sampleRuns:finalize"} {
		if got[path]["sampleRunId"] != "sample-1" {
			t.Fatalf("%s sampleRunId = %#v, want sample-1 metadata", path, got[path]["sampleRunId"])
		}
	}
}

// A missing RunID fails closed before anything is sent; SampleRunID is never
// used in its place.
func TestClients_MissingRunIDFailsClosed(t *testing.T) {
	grpcClient, rec := newRunIDRecorderClient(t)
	httpClient := NewHTTPClientWithClient("http://ah.test", &http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			t.Errorf("request sent without RunID: %s %s", r.Method, r.URL)
			return jsonResponse(http.StatusOK, `{}`), nil
		}),
	})
	for name, c := range map[string]Client{"grpc": grpcClient, "http": httpClient} {
		for _, runID := range []string{"", "  "} {
			for op, err := range callAll(c, runID) {
				if !errors.Is(err, ErrMissingRunID) {
					t.Fatalf("%s %s with runID %q: error = %v, want ErrMissingRunID", name, op, runID, err)
				}
			}
		}
	}
	rec.mu.Lock()
	defer rec.mu.Unlock()
	if rec.calls != 0 {
		t.Fatalf("gRPC server received %d calls without RunID, want 0", rec.calls)
	}
}
