package handoff

import (
	"context"
	"net"
	"testing"

	ahv1 "github.com/HeaInSeo/JUMI/pkg/handoff/ahv1"
	"github.com/HeaInSeo/JUMI/pkg/metrics"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
)

// newTestGRPCClientConn creates a GRPCClient connected to a bufconn stub server.
func newTestGRPCClientConn(t *testing.T) *GRPCClient {
	t.Helper()
	listener := bufconn.Listen(1024 * 1024)
	server := grpc.NewServer()
	stub := &stubResolverServer{}
	ahv1.RegisterArtifactHandoffResolverServer(server, stub)
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
	return &GRPCClient{conn: conn, client: ahv1.NewArtifactHandoffResolverClient(conn)}
}

func TestGRPCClient_NotifyNodeTerminal(t *testing.T) {
	c := newTestGRPCClientConn(t)
	if err := c.NotifyNodeTerminal(context.Background(), NotifyNodeTerminalRequest{
		SampleRunID:   "s1",
		NodeID:        "n1",
		AttemptID:     "a1",
		TerminalState: "Succeeded",
	}); err != nil {
		t.Fatalf("NotifyNodeTerminal() error = %v", err)
	}
}

func TestGRPCClient_FinalizeSampleRun(t *testing.T) {
	c := newTestGRPCClientConn(t)
	if err := c.FinalizeSampleRun(context.Background(), FinalizeSampleRunRequest{
		SampleRunID: "s1",
	}); err != nil {
		t.Fatalf("FinalizeSampleRun() error = %v", err)
	}
}

func TestGRPCClient_EvaluateGC(t *testing.T) {
	c := newTestGRPCClientConn(t)
	if err := c.EvaluateGC(context.Background(), EvaluateGCRequest{
		SampleRunID: "s1",
	}); err != nil {
		t.Fatalf("EvaluateGC() error = %v", err)
	}
}

func TestGRPCClient_SetMetrics(t *testing.T) {
	c := newTestGRPCClientConn(t)
	m, err := metrics.New()
	if err != nil {
		t.Fatalf("metrics.New() error = %v", err)
	}
	c.SetMetrics(m)

	// Resolve through metrics-enabled client to cover the IncHandoffResolve path.
	if _, err := c.ResolveBinding(context.Background(), ResolveBindingRequest{
		BindingName:        "b",
		ProducerNodeID:     "p",
		ProducerOutputName: "out",
	}); err != nil {
		t.Fatalf("ResolveBinding(with metrics) error = %v", err)
	}
}

func TestGRPCClient_Close_NilSafe(t *testing.T) {
	var c *GRPCClient
	if err := c.Close(); err != nil {
		t.Fatalf("Close(nil) error = %v", err)
	}
}

// --- grpcArtifactLocationFromClient (pure helper) ---

func TestGrpcArtifactLocationFromClient_NodeLocal(t *testing.T) {
	loc := grpcArtifactLocationFromClient(ArtifactLocation{
		NodeLocal: &NodeLocalLocation{NodeName: "node-a", Path: "/data"},
	})
	if loc == nil {
		t.Fatal("expected non-nil location")
	}
	nl := loc.GetNodeLocal()
	if nl == nil {
		t.Fatal("expected NodeLocal backend")
	}
	if nl.GetNodeName() != "node-a" {
		t.Fatalf("NodeName = %q, want node-a", nl.GetNodeName())
	}
}

func TestGrpcArtifactLocationFromClient_HTTP(t *testing.T) {
	loc := grpcArtifactLocationFromClient(ArtifactLocation{
		HTTP: &HTTPSource{URI: "http://example.com/file"},
	})
	if loc == nil {
		t.Fatal("expected non-nil location")
	}
	if loc.GetHttp() == nil {
		t.Fatal("expected HTTP backend")
	}
	if loc.GetHttp().GetUri() != "http://example.com/file" {
		t.Fatalf("URI = %q, want http://example.com/file", loc.GetHttp().GetUri())
	}
}

func TestGrpcArtifactLocationFromClient_Empty(t *testing.T) {
	loc := grpcArtifactLocationFromClient(ArtifactLocation{})
	if loc != nil {
		t.Fatalf("expected nil for empty location, got %v", loc)
	}
}

// --- grpcArtifactLocations ---

func TestGrpcArtifactLocations_Empty(t *testing.T) {
	if got := grpcArtifactLocations(nil); got != nil {
		t.Fatalf("grpcArtifactLocations(nil) = %v, want nil", got)
	}
	if got := grpcArtifactLocations([]ArtifactLocation{}); got != nil {
		t.Fatalf("grpcArtifactLocations(empty) = %v, want nil", got)
	}
}

func TestGrpcArtifactLocations_SkipsEmpty(t *testing.T) {
	locs := []ArtifactLocation{
		{}, // no backend — should be skipped
		{NodeLocal: &NodeLocalLocation{NodeName: "n", Path: "/p"}},
	}
	got := grpcArtifactLocations(locs)
	if len(got) != 1 {
		t.Fatalf("grpcArtifactLocations len = %d, want 1 (empty location skipped)", len(got))
	}
}

// --- grpcArtifactLocation (reverse direction) ---

func TestGrpcArtifactLocation_Nil(t *testing.T) {
	if got := grpcArtifactLocation(nil); got != nil {
		t.Fatalf("grpcArtifactLocation(nil) = %v, want nil", got)
	}
}

func TestGrpcArtifactLocation_Unknown(t *testing.T) {
	// Passing an ArtifactLocation with no backend should return nil.
	proto := &ahv1.ArtifactLocation{}
	if got := grpcArtifactLocation(proto); got != nil {
		t.Fatalf("grpcArtifactLocation(no backend) = %v, want nil", got)
	}
}

// --- grpcMaterializationCandidates ---

func TestGrpcMaterializationCandidates_Empty(t *testing.T) {
	if got := grpcMaterializationCandidates(nil); got != nil {
		t.Fatalf("grpcMaterializationCandidates(nil) = %v, want nil", got)
	}
}

func TestGrpcMaterializationCandidates_SkipsNil(t *testing.T) {
	candidates := []*ahv1.MaterializationCandidate{nil}
	got := grpcMaterializationCandidates(candidates)
	if len(got) != 0 {
		t.Fatalf("expected nil candidates to be skipped, got len %d", len(got))
	}
}

func TestGrpcMaterializationCandidates_WithConditions(t *testing.T) {
	candidates := []*ahv1.MaterializationCandidate{
		{
			Priority:       1,
			Mode:           "local_reuse",
			ExpectedDigest: "sha256:abc",
			Conditions: []*ahv1.MaterializationCondition{
				{Kind: "scheduled_on_node", NodeName: "node-a"},
			},
		},
	}
	got := grpcMaterializationCandidates(candidates)
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
	if got[0].Mode != "local_reuse" {
		t.Fatalf("Mode = %q, want local_reuse", got[0].Mode)
	}
	if len(got[0].Conditions) != 1 {
		t.Fatalf("Conditions len = %d, want 1", len(got[0].Conditions))
	}
	if got[0].Conditions[0].Kind != "scheduled_on_node" {
		t.Fatalf("Conditions[0].Kind = %q, want scheduled_on_node", got[0].Conditions[0].Kind)
	}
}
