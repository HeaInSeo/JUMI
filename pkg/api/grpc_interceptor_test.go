package api

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/HeaInSeo/JUMI/pkg/executor"
	"github.com/HeaInSeo/JUMI/pkg/registry"
	"github.com/HeaInSeo/JUMI/pkg/spec"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
)

// passthroughInterceptor exercises the interceptor code path in each handler.
func passthroughInterceptor(ctx context.Context, req any, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
	return handler(ctx, req)
}

func newInterceptorClient(t *testing.T, reg *registry.MemoryRegistry) *RunServiceClient {
	t.Helper()
	eng := executor.NewNoopEngine(reg)
	svc := NewService(reg, eng)

	listener := bufconn.Listen(1024 * 1024)
	server := grpc.NewServer(grpc.UnaryInterceptor(passthroughInterceptor))
	RegisterRunService(server, svc)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)

	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
			return listener.Dial()
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultCallOptions(grpc.CallContentSubtype("json")),
	)
	if err != nil {
		t.Fatalf("grpc.NewClient() error = %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return NewRunServiceClient(conn)
}

func TestGRPCInterceptor_AllHandlers(t *testing.T) {
	reg := registry.NewMemoryRegistry()
	client := newInterceptorClient(t, reg)
	ctx := context.Background()

	// SubmitRun via interceptor
	submitResp, err := client.SubmitRun(ctx, &SubmitRunRequest{
		Spec: spec.ExecutableRunSpec{
			Run:   spec.RunMetadata{RunID: "irun-1", SubmittedAt: time.Now().UTC()},
			Graph: spec.Graph{Nodes: []spec.Node{{NodeID: "a", Image: "img:1"}}},
		},
	}, grpc.CallContentSubtype("json"))
	if err != nil {
		t.Fatalf("SubmitRun(interceptor) error = %v", err)
	}
	if submitResp.RunID != "irun-1" {
		t.Fatalf("SubmitRun RunID = %q, want irun-1", submitResp.RunID)
	}

	// GetRun via interceptor
	getResp, err := client.GetRun(ctx, &GetRunRequest{RunID: "irun-1"}, grpc.CallContentSubtype("json"))
	if err != nil {
		t.Fatalf("GetRun(interceptor) error = %v", err)
	}
	if getResp.Run.RunID != "irun-1" {
		t.Fatalf("GetRun RunID = %q, want irun-1", getResp.Run.RunID)
	}

	// ListRunNodes via interceptor
	nodesResp, err := client.ListRunNodes(ctx, &ListRunNodesRequest{RunID: "irun-1"}, grpc.CallContentSubtype("json"))
	if err != nil {
		t.Fatalf("ListRunNodes(interceptor) error = %v", err)
	}
	if len(nodesResp.Nodes) != 1 {
		t.Fatalf("ListRunNodes len = %d, want 1", len(nodesResp.Nodes))
	}

	// ListNodeAttempts via interceptor
	_, err = client.ListNodeAttempts(ctx, &ListNodeAttemptsRequest{RunID: "irun-1", NodeID: "a"}, grpc.CallContentSubtype("json"))
	if err != nil {
		t.Fatalf("ListNodeAttempts(interceptor) error = %v", err)
	}

	// ListRunEvents via interceptor
	_, err = client.ListRunEvents(ctx, &ListRunEventsRequest{RunID: "irun-1"}, grpc.CallContentSubtype("json"))
	if err != nil {
		t.Fatalf("ListRunEvents(interceptor) error = %v", err)
	}

	// CancelRun via interceptor
	cancelResp, err := client.CancelRun(ctx, &CancelRunRequest{RunID: "irun-1", Reason: "test"}, grpc.CallContentSubtype("json"))
	if err != nil {
		t.Fatalf("CancelRun(interceptor) error = %v", err)
	}
	if !cancelResp.Accepted {
		t.Fatal("CancelRun accepted = false, want true")
	}
}
