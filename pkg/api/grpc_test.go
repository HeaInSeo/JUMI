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

func TestRunServiceGRPCSubmitAndGet(t *testing.T) {
	reg := registry.NewMemoryRegistry()
	eng := executor.NewNoopEngine(reg)
	svc := NewService(reg, eng)

	listener := bufconn.Listen(1024 * 1024)
	server := grpc.NewServer()
	RegisterRunService(server, svc)
	go func() {
		_ = server.Serve(listener)
	}()
	defer server.Stop()

	ctx := context.Background()
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
	defer conn.Close()

	client := NewRunServiceClient(conn)
	resp, err := client.SubmitRun(ctx, &SubmitRunRequest{Spec: spec.ExecutableRunSpec{
		Run: spec.RunMetadata{
			RunID:       "grpc-run-1",
			SubmittedAt: time.Now().UTC(),
			FailurePolicy: spec.FailurePolicy{
				Mode: "fail-fast",
			},
		},
		Graph: spec.Graph{Nodes: []spec.Node{{NodeID: "a", Image: "busybox:1.36"}}},
	}}, grpc.CallContentSubtype("json"))
	if err != nil {
		t.Fatalf("SubmitRun() error = %v", err)
	}
	if resp.RunID != "grpc-run-1" {
		t.Fatalf("SubmitRun() runID = %q, want grpc-run-1", resp.RunID)
	}

	getResp, err := client.GetRun(ctx, &GetRunRequest{RunID: "grpc-run-1"}, grpc.CallContentSubtype("json"))
	if err != nil {
		t.Fatalf("GetRun() error = %v", err)
	}
	if getResp.Run.Status != spec.RunStatusAdmitted {
		t.Fatalf("GetRun() status = %q, want %q", getResp.Run.Status, spec.RunStatusAdmitted)
	}
}
