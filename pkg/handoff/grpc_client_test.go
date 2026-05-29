package handoff

import (
	"context"
	"net"
	"testing"

	ahv1 "github.com/HeaInSeo/JUMI/pkg/handoff/ahv1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
)

func TestGRPCClientRoundTrip(t *testing.T) {
	listener := bufconn.Listen(1024 * 1024)
	server := grpc.NewServer()
	stub := &stubResolverServer{}
	ahv1.RegisterArtifactHandoffResolverServer(server, stub)
	go func() {
		_ = server.Serve(listener)
	}()
	defer server.Stop()

	origDialer := grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
		return listener.Dial()
	})
	conn, err := grpc.NewClient("passthrough:///bufnet", origDialer, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("grpc.NewClient() error = %v", err)
	}
	client := &GRPCClient{conn: conn, client: ahv1.NewArtifactHandoffResolverClient(conn)}
	defer func() {
		_ = client.Close()
	}()

	resolved, err := client.ResolveBinding(context.Background(), ResolveBindingRequest{
		SampleRunID:        "sample-1",
		BindingName:        "dataset",
		ProducerNodeID:     "node-a",
		ProducerOutputName: "output",
	})
	if err != nil {
		t.Fatalf("ResolveBinding() error = %v", err)
	}
	if resolved.Decision != "remote_fetch" {
		t.Fatalf("decision = %q, want remote_fetch", resolved.Decision)
	}
	if resolved.PlacementIntent.NodeName != "node-a" {
		t.Fatalf("placement node = %q, want node-a", resolved.PlacementIntent.NodeName)
	}
	if resolved.MaterializationPlan.Mode != "remote_fetch" {
		t.Fatalf("materialization mode = %q, want remote_fetch", resolved.MaterializationPlan.Mode)
	}
	if resolved.MaterializationPlan.ExpectedSize != 17 {
		t.Fatalf("materialization expected size = %d, want 17", resolved.MaterializationPlan.ExpectedSize)
	}
	if resolved.MaterializationPlan.SourceLocation == nil || resolved.MaterializationPlan.SourceLocation.HTTP == nil {
		t.Fatalf("sourceLocation = %#v, want http source", resolved.MaterializationPlan.SourceLocation)
	}
	if resolved.MaterializationPlan.LocalPath != "/work/inputs/dataset" {
		t.Fatalf("localPath = %q, want /work/inputs/dataset", resolved.MaterializationPlan.LocalPath)
	}
	if err := client.RegisterArtifact(context.Background(), RegisterArtifactRequest{
		SampleRunID:       "sample-1",
		ProducerNodeID:    "node-a",
		ProducerAttemptID: "attempt-1",
		OutputName:        "output",
		LogicalURI:        "jumi://runs/sample-1/nodes/node-a/outputs/output",
		Locations: []ArtifactLocation{{
			NodeLocal: &NodeLocalLocation{NodeName: "node-a", Path: "/jumi-node-artifacts/cas/sha256/abc"},
		}},
		SizeBytes:         2048,
	}); err != nil {
		t.Fatalf("RegisterArtifact() error = %v", err)
	}
	if stub.lastRegister == nil {
		t.Fatal("lastRegister = nil, want request")
	}
	if stub.lastRegister.GetArtifact().GetSizeBytes() != 2048 {
		t.Fatalf("artifact sizeBytes = %d, want 2048", stub.lastRegister.GetArtifact().GetSizeBytes())
	}
	if stub.lastRegister.GetArtifact().GetProducerAttemptId() != "attempt-1" {
		t.Fatalf("producer attempt = %q, want attempt-1", stub.lastRegister.GetArtifact().GetProducerAttemptId())
	}
	if stub.lastRegister.GetArtifact().GetLogicalUri() != "jumi://runs/sample-1/nodes/node-a/outputs/output" {
		t.Fatalf("logical URI = %q, want logical URI", stub.lastRegister.GetArtifact().GetLogicalUri())
	}
	if len(stub.lastRegister.GetArtifact().GetLocations()) != 1 {
		t.Fatalf("locations len = %d, want 1", len(stub.lastRegister.GetArtifact().GetLocations()))
	}
}

type stubResolverServer struct {
	ahv1.UnimplementedArtifactHandoffResolverServer
	lastRegister *ahv1.RegisterArtifactRequest
}

func (stubResolverServer) ResolveHandoff(context.Context, *ahv1.ResolveHandoffRequest) (*ahv1.ResolveHandoffResponse, error) {
	return &ahv1.ResolveHandoffResponse{
		ResolutionStatus: "RESOLVED",
		Decision:         "remote_fetch",
		PlacementIntent: &ahv1.PlacementIntent{
			Mode:     "required_node",
			NodeName: "node-a",
		},
		MaterializationPlan: &ahv1.MaterializationPlan{
			Mode:              "remote_fetch",
			Uri:               "http://artifact.local/output",
			ExpectedDigest:    "sha256:abc",
			ExpectedSizeBytes: 17,
			SourceLocation: &ahv1.ArtifactLocation{
				Backend: &ahv1.ArtifactLocation_Http{
					Http: &ahv1.HttpSource{Uri: "http://artifact.local/output"},
				},
			},
			LocalPath: "/work/inputs/dataset",
		},
	}, nil
}

func (s *stubResolverServer) RegisterArtifact(_ context.Context, req *ahv1.RegisterArtifactRequest) (*ahv1.RegisterArtifactResponse, error) {
	s.lastRegister = req
	return &ahv1.RegisterArtifactResponse{AvailabilityState: "LOCAL_ONLY"}, nil
}

func (stubResolverServer) NotifyNodeTerminal(context.Context, *ahv1.NotifyNodeTerminalRequest) (*ahv1.NotifyNodeTerminalResponse, error) {
	return &ahv1.NotifyNodeTerminalResponse{Accepted: true}, nil
}

func (stubResolverServer) FinalizeSampleRun(context.Context, *ahv1.FinalizeSampleRunRequest) (*ahv1.FinalizeSampleRunResponse, error) {
	return &ahv1.FinalizeSampleRunResponse{Accepted: true}, nil
}

func (stubResolverServer) EvaluateGC(context.Context, *ahv1.EvaluateGCRequest) (*ahv1.EvaluateGCResponse, error) {
	return &ahv1.EvaluateGCResponse{Accepted: true}, nil
}
