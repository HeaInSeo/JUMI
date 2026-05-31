package api

import (
	"context"
	"errors"
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

func newTestService(t *testing.T) (*Service, *registry.MemoryRegistry) {
	t.Helper()
	reg := registry.NewMemoryRegistry()
	eng := executor.NewNoopEngine(reg)
	return NewService(reg, eng), reg
}

func seedRun(t *testing.T, svc *Service, runID string) {
	t.Helper()
	_, err := svc.SubmitRun(context.Background(), SubmitRunRequest{
		Spec: spec.ExecutableRunSpec{
			Run:   spec.RunMetadata{RunID: runID, SubmittedAt: time.Now().UTC()},
			Graph: spec.Graph{Nodes: []spec.Node{{NodeID: "a", Image: "img:1"}}},
		},
	})
	if err != nil {
		t.Fatalf("seedRun SubmitRun() error = %v", err)
	}
}

// --- GetRun error paths ---

func TestService_GetRun_EmptyRunID(t *testing.T) {
	svc, _ := newTestService(t)
	_, err := svc.GetRun(context.Background(), GetRunRequest{RunID: ""})
	if err == nil {
		t.Fatal("expected error for empty runId")
	}
}

func TestService_GetRun_NotFound(t *testing.T) {
	svc, _ := newTestService(t)
	_, err := svc.GetRun(context.Background(), GetRunRequest{RunID: "missing"})
	if !errors.Is(err, registry.ErrRunNotFound) {
		t.Fatalf("expected ErrRunNotFound, got %v", err)
	}
}

// --- ListRunNodes error paths ---

func TestService_ListRunNodes_EmptyRunID(t *testing.T) {
	svc, _ := newTestService(t)
	_, err := svc.ListRunNodes(context.Background(), ListRunNodesRequest{RunID: ""})
	if err == nil {
		t.Fatal("expected error for empty runId")
	}
}

func TestService_ListRunNodes_NotFound(t *testing.T) {
	svc, _ := newTestService(t)
	_, err := svc.ListRunNodes(context.Background(), ListRunNodesRequest{RunID: "missing"})
	if !errors.Is(err, registry.ErrRunNotFound) {
		t.Fatalf("expected ErrRunNotFound, got %v", err)
	}
}

// --- ListNodeAttempts error paths ---

func TestService_ListNodeAttempts_EmptyRunID(t *testing.T) {
	svc, _ := newTestService(t)
	_, err := svc.ListNodeAttempts(context.Background(), ListNodeAttemptsRequest{RunID: "", NodeID: "a"})
	if err == nil {
		t.Fatal("expected error for empty runId")
	}
}

func TestService_ListNodeAttempts_EmptyNodeID(t *testing.T) {
	svc, _ := newTestService(t)
	_, err := svc.ListNodeAttempts(context.Background(), ListNodeAttemptsRequest{RunID: "run-1", NodeID: ""})
	if err == nil {
		t.Fatal("expected error for empty nodeId")
	}
}

// --- ListRunEvents error paths ---

func TestService_ListRunEvents_EmptyRunID(t *testing.T) {
	svc, _ := newTestService(t)
	_, err := svc.ListRunEvents(context.Background(), ListRunEventsRequest{RunID: ""})
	if err == nil {
		t.Fatal("expected error for empty runId")
	}
}

func TestService_ListRunEvents_NotFound(t *testing.T) {
	svc, _ := newTestService(t)
	_, err := svc.ListRunEvents(context.Background(), ListRunEventsRequest{RunID: "missing"})
	if !errors.Is(err, registry.ErrRunNotFound) {
		t.Fatalf("expected ErrRunNotFound, got %v", err)
	}
}

// --- CancelRun error paths ---

func TestService_CancelRun_EmptyRunID(t *testing.T) {
	svc, _ := newTestService(t)
	_, err := svc.CancelRun(context.Background(), CancelRunRequest{RunID: ""})
	if err == nil {
		t.Fatal("expected error for empty runId")
	}
}

func TestService_CancelRun_NotFound(t *testing.T) {
	svc, _ := newTestService(t)
	_, err := svc.CancelRun(context.Background(), CancelRunRequest{RunID: "missing"})
	if err == nil {
		t.Fatal("expected error for missing run")
	}
}

// --- SubmitRun error paths ---

func TestService_SubmitRun_InvalidSpec(t *testing.T) {
	svc, _ := newTestService(t)
	// Missing runId should fail validation.
	_, err := svc.SubmitRun(context.Background(), SubmitRunRequest{
		Spec: spec.ExecutableRunSpec{
			Run:   spec.RunMetadata{RunID: ""},
			Graph: spec.Graph{Nodes: []spec.Node{{NodeID: "a", Image: "img:1"}}},
		},
	})
	if err == nil {
		t.Fatal("expected error for invalid spec")
	}
}

func TestService_SubmitRun_DuplicateRunID(t *testing.T) {
	svc, _ := newTestService(t)
	seedRun(t, svc, "dup-run")
	_, err := svc.SubmitRun(context.Background(), SubmitRunRequest{
		Spec: spec.ExecutableRunSpec{
			Run:   spec.RunMetadata{RunID: "dup-run", SubmittedAt: time.Now().UTC()},
			Graph: spec.Graph{Nodes: []spec.Node{{NodeID: "a", Image: "img:1"}}},
		},
	})
	if !errors.Is(err, registry.ErrRunAlreadyExists) {
		t.Fatalf("expected ErrRunAlreadyExists, got %v", err)
	}
}

// --- gRPC ListRunNodes and CancelRun via wire ---

func newTestGRPCClient(t *testing.T) (*RunServiceClient, *registry.MemoryRegistry) {
	t.Helper()
	reg := registry.NewMemoryRegistry()
	eng := executor.NewNoopEngine(reg)
	svc := NewService(reg, eng)

	listener := bufconn.Listen(1024 * 1024)
	server := grpc.NewServer()
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
	return NewRunServiceClient(conn), reg
}

func TestGRPC_ListRunNodes(t *testing.T) {
	client, reg := newTestGRPCClient(t)
	ctx := context.Background()

	// Seed a run directly in the registry so no engine side-effect needed.
	run := spec.RunRecord{RunID: "grpc-nodes-run", Status: spec.RunStatusAccepted, AcceptedAt: time.Now().UTC()}
	nodes := []spec.NodeRecord{
		{RunID: "grpc-nodes-run", NodeID: "n1", Status: spec.NodeStatusPending},
		{RunID: "grpc-nodes-run", NodeID: "n2", Status: spec.NodeStatusPending},
	}
	if err := reg.CreateRun(ctx, run, nodes); err != nil {
		t.Fatalf("CreateRun() error = %v", err)
	}

	resp, err := client.ListRunNodes(ctx, &ListRunNodesRequest{RunID: "grpc-nodes-run"}, grpc.CallContentSubtype("json"))
	if err != nil {
		t.Fatalf("ListRunNodes() error = %v", err)
	}
	if len(resp.Nodes) != 2 {
		t.Fatalf("ListRunNodes() len = %d, want 2", len(resp.Nodes))
	}
}

func TestGRPC_CancelRun(t *testing.T) {
	client, _ := newTestGRPCClient(t)
	ctx := context.Background()

	// Submit a run via gRPC so engine registers it.
	_, err := client.SubmitRun(ctx, &SubmitRunRequest{
		Spec: spec.ExecutableRunSpec{
			Run:   spec.RunMetadata{RunID: "grpc-cancel-run", SubmittedAt: time.Now().UTC()},
			Graph: spec.Graph{Nodes: []spec.Node{{NodeID: "a", Image: "img:1"}}},
		},
	}, grpc.CallContentSubtype("json"))
	if err != nil {
		t.Fatalf("SubmitRun() error = %v", err)
	}

	cancelResp, err := client.CancelRun(ctx, &CancelRunRequest{RunID: "grpc-cancel-run", Reason: "test"}, grpc.CallContentSubtype("json"))
	if err != nil {
		t.Fatalf("CancelRun() error = %v", err)
	}
	if !cancelResp.Accepted {
		t.Fatal("CancelRun() accepted = false, want true")
	}
}
