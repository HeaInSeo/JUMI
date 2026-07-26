package handoff

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	ahv1 "github.com/HeaInSeo/JUMI/pkg/handoff/ahv1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
)

// --- withDefaultTimeout (pure helper) ---

func TestWithDefaultTimeout_NoDeadlineAppliesDefault(t *testing.T) {
	ctx, cancel := withDefaultTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	select {
	case <-ctx.Done():
		t.Fatal("ctx.Done() fired before the default timeout elapsed")
	default:
	}
	<-ctx.Done()
	if !errors.Is(ctx.Err(), context.DeadlineExceeded) {
		t.Fatalf("ctx.Err() = %v, want context.DeadlineExceeded", ctx.Err())
	}
}

func TestWithDefaultTimeout_ExistingDeadlinePreserved(t *testing.T) {
	parent, parentCancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer parentCancel()
	wantDeadline, ok := parent.Deadline()
	if !ok {
		t.Fatal("parent context unexpectedly has no deadline")
	}

	// Ask for a much shorter default; it must NOT override the caller's
	// existing deadline in either direction.
	ctx, cancel := withDefaultTimeout(parent, time.Millisecond)
	defer cancel()

	gotDeadline, ok := ctx.Deadline()
	if !ok {
		t.Fatal("expected returned context to still carry a deadline")
	}
	if !gotDeadline.Equal(wantDeadline) {
		t.Fatalf("deadline = %v, want %v (caller's deadline must be left untouched)", gotDeadline, wantDeadline)
	}
}

// --- end-to-end: an unresponsive server must not hang a call forever ---

// hangingResolverServer simulates an artifact-handoff process that accepts
// the RPC but never responds on its own. It only returns once the (server-
// side, deadline-propagated) context is done, mirroring a network partition
// that silently drops packets rather than resetting the connection.
type hangingResolverServer struct {
	ahv1.UnimplementedArtifactHandoffResolverServer
}

func (hangingResolverServer) ResolveHandoff(ctx context.Context, _ *ahv1.ResolveHandoffRequest) (*ahv1.ResolveHandoffResponse, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

func (hangingResolverServer) NotifyNodeTerminal(ctx context.Context, _ *ahv1.NotifyNodeTerminalRequest) (*ahv1.NotifyNodeTerminalResponse, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

func newHangingTestClient(t *testing.T) *GRPCClient {
	t.Helper()
	listener := bufconn.Listen(1024 * 1024)
	server := grpc.NewServer()
	ahv1.RegisterArtifactHandoffResolverServer(server, hangingResolverServer{})
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

// TestGRPCClient_NoCallerDeadline_BoundedByDefaultTimeout is the regression
// test for the issue: previously, calling a GRPCClient method with an
// undeadlined context (e.g. context.Background(), which several pkg/executor
// cleanup paths use) against an unresponsive server would hang indefinitely.
// With the fix, the client applies its own default timeout and returns.
func TestGRPCClient_NoCallerDeadline_BoundedByDefaultTimeout(t *testing.T) {
	origResolve, origCall := defaultResolveBindingTimeout, defaultCallTimeout
	defaultResolveBindingTimeout = 100 * time.Millisecond
	defaultCallTimeout = 100 * time.Millisecond
	t.Cleanup(func() {
		defaultResolveBindingTimeout = origResolve
		defaultCallTimeout = origCall
	})

	client := newHangingTestClient(t)

	start := time.Now()
	_, err := client.ResolveBinding(context.Background(), ResolveBindingRequest{
		BindingName:        "b",
		ProducerNodeID:     "producer",
		ProducerOutputName: "out",
	})
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("ResolveBinding() error = nil, want a deadline-exceeded error from the internal default timeout")
	}
	if elapsed > 5*time.Second {
		t.Fatalf("ResolveBinding() took %v, want it bounded by the ~100ms default timeout (not an indefinite hang)", elapsed)
	}
	t.Logf("ResolveBinding() against a hanging server returned after %v: %v", elapsed, err)

	start = time.Now()
	err = client.NotifyNodeTerminal(context.Background(), NotifyNodeTerminalRequest{
		SampleRunID:   "s1",
		NodeID:        "n1",
		TerminalState: "Succeeded",
	})
	elapsed = time.Since(start)

	if err == nil {
		t.Fatal("NotifyNodeTerminal() error = nil, want a deadline-exceeded error from the internal default timeout")
	}
	if elapsed > 5*time.Second {
		t.Fatalf("NotifyNodeTerminal() took %v, want it bounded by the ~100ms default timeout (not an indefinite hang)", elapsed)
	}
	t.Logf("NotifyNodeTerminal() against a hanging server returned after %v: %v", elapsed, err)
}

// TestGRPCClient_CallerDeadlineNotOverridden proves the safety net does not
// clobber a caller-supplied deadline: even though the internal default is set
// much larger here, a caller-provided short deadline still governs how long
// the call actually takes.
func TestGRPCClient_CallerDeadlineNotOverridden(t *testing.T) {
	origResolve := defaultResolveBindingTimeout
	defaultResolveBindingTimeout = 5 * time.Second
	t.Cleanup(func() { defaultResolveBindingTimeout = origResolve })

	client := newHangingTestClient(t)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err := client.ResolveBinding(ctx, ResolveBindingRequest{
		BindingName:        "b",
		ProducerNodeID:     "producer",
		ProducerOutputName: "out",
	})
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("ResolveBinding() error = nil, want the caller's short deadline to fire")
	}
	if elapsed > 2*time.Second {
		t.Fatalf("ResolveBinding() took %v, want it bounded by the caller's 100ms deadline rather than the 5s default", elapsed)
	}
}
