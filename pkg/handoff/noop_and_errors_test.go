package handoff

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

// --- NoopClient ---

func TestNoopClient_ResolveBinding(t *testing.T) {
	c := NewNoopClient()
	resp, err := c.ResolveBinding(context.Background(), ResolveBindingRequest{
		BindingName:        "b",
		ChildNodeID:        "child",
		ProducerNodeID:     "producer",
		ProducerOutputName: "out",
		TargetNodeName:     "target-node",
	})
	if err != nil {
		t.Fatalf("ResolveBinding() error = %v", err)
	}
	if resp.ResolutionStatus != "RESOLVED" {
		t.Fatalf("status = %q, want RESOLVED", resp.ResolutionStatus)
	}
	if resp.PlacementIntent.NodeName != "target-node" {
		t.Fatalf("placement node = %q, want target-node", resp.PlacementIntent.NodeName)
	}
	if resp.MaterializationPlan.Mode != "none" {
		t.Fatalf("materialization mode = %q, want none", resp.MaterializationPlan.Mode)
	}
}

func TestNoopClient_ResolveBinding_FallsBackToProducerNodeID(t *testing.T) {
	c := NewNoopClient()
	resp, err := c.ResolveBinding(context.Background(), ResolveBindingRequest{
		BindingName:        "b",
		ChildNodeID:        "child",
		ProducerNodeID:     "producer",
		ProducerOutputName: "out",
		// TargetNodeName not set — should fall back to ProducerNodeID
	})
	if err != nil {
		t.Fatalf("ResolveBinding() error = %v", err)
	}
	if resp.PlacementIntent.NodeName != "producer" {
		t.Fatalf("placement node = %q, want producer (fallback)", resp.PlacementIntent.NodeName)
	}
}

func TestNoopClient_RegisterArtifact(t *testing.T) {
	c := NewNoopClient()
	err := c.RegisterArtifact(context.Background(), RegisterArtifactRequest{
		SampleRunID:    "s1",
		ProducerNodeID: "n1",
		OutputName:     "out",
	})
	if err != nil {
		t.Fatalf("RegisterArtifact() error = %v", err)
	}
}

func TestNoopClient_NotifyNodeTerminal(t *testing.T) {
	c := NewNoopClient()
	err := c.NotifyNodeTerminal(context.Background(), NotifyNodeTerminalRequest{
		SampleRunID:   "s1",
		NodeID:        "n1",
		TerminalState: "Succeeded",
	})
	if err != nil {
		t.Fatalf("NotifyNodeTerminal() error = %v", err)
	}
}

func TestNoopClient_FinalizeSampleRun(t *testing.T) {
	c := NewNoopClient()
	err := c.FinalizeSampleRun(context.Background(), FinalizeSampleRunRequest{SampleRunID: "s1"})
	if err != nil {
		t.Fatalf("FinalizeSampleRun() error = %v", err)
	}
}

func TestNoopClient_EvaluateGC(t *testing.T) {
	c := NewNoopClient()
	err := c.EvaluateGC(context.Background(), EvaluateGCRequest{SampleRunID: "s1"})
	if err != nil {
		t.Fatalf("EvaluateGC() error = %v", err)
	}
}

func TestNoopClient_GetSampleRunLifecycle(t *testing.T) {
	c := NewNoopClient()
	lc, ok, err := c.GetSampleRunLifecycle(context.Background(), GetSampleRunLifecycleRequest{SampleRunID: "s1"})
	if err != nil {
		t.Fatalf("GetSampleRunLifecycle() error = %v", err)
	}
	if ok {
		t.Fatal("GetSampleRunLifecycle() ok = true, want false for noop")
	}
	if lc.SampleRunID != "s1" {
		t.Fatalf("SampleRunID = %q, want s1", lc.SampleRunID)
	}
}

// --- HTTPError ---

func TestHTTPError_Error(t *testing.T) {
	e := &HTTPError{StatusCode: 500, Op: "resolve"}
	msg := e.Error()
	if !strings.Contains(msg, "500") {
		t.Fatalf("error message %q missing status code 500", msg)
	}
	if !strings.Contains(msg, "resolve") {
		t.Fatalf("error message %q missing op 'resolve'", msg)
	}
}

// --- NewHTTPClient ---

func TestNewHTTPClient_DefaultTimeout(t *testing.T) {
	c := NewHTTPClient("http://example.com", 0) // 0 => uses default 5s
	if c == nil {
		t.Fatal("NewHTTPClient() returned nil")
	}
}

// --- HTTP error response paths ---

func TestHTTPClient_ResolveBinding_ServerError(t *testing.T) {
	c := NewHTTPClientWithClient("http://ah.test", &http.Client{
		Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: 503,
				Header:     http.Header{},
				Body:       io.NopCloser(strings.NewReader("")),
			}, nil
		}),
	})
	_, err := c.ResolveBinding(context.Background(), ResolveBindingRequest{
		BindingName: "b", ChildNodeID: "c", ProducerNodeID: "p", ProducerOutputName: "o",
	})
	if err == nil {
		t.Fatal("expected error for 503 response")
	}
	var httpErr *HTTPError
	if !isHTTPError(err, &httpErr) {
		t.Fatalf("expected *HTTPError, got %T: %v", err, err)
	}
	if httpErr.StatusCode != 503 {
		t.Fatalf("HTTPError.StatusCode = %d, want 503", httpErr.StatusCode)
	}
}

func TestHTTPClient_RegisterArtifact_ServerError(t *testing.T) {
	c := NewHTTPClientWithClient("http://ah.test", &http.Client{
		Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: 400,
				Header:     http.Header{},
				Body:       io.NopCloser(strings.NewReader("")),
			}, nil
		}),
	})
	err := c.RegisterArtifact(context.Background(), RegisterArtifactRequest{
		SampleRunID: "s1", ProducerNodeID: "n1", OutputName: "out",
	})
	if err == nil {
		t.Fatal("expected error for 400 response")
	}
	var httpErr *HTTPError
	if !isHTTPError(err, &httpErr) {
		t.Fatalf("expected *HTTPError, got %T", err)
	}
	if httpErr.StatusCode != 400 {
		t.Fatalf("HTTPError.StatusCode = %d, want 400", httpErr.StatusCode)
	}
}

func TestHTTPClient_NotifyNodeTerminal_ServerError(t *testing.T) {
	c := NewHTTPClientWithClient("http://ah.test", &http.Client{
		Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: 502,
				Header:     http.Header{},
				Body:       io.NopCloser(strings.NewReader("")),
			}, nil
		}),
	})
	err := c.NotifyNodeTerminal(context.Background(), NotifyNodeTerminalRequest{
		SampleRunID: "s1", NodeID: "n1", TerminalState: "Succeeded",
	})
	if err == nil {
		t.Fatal("expected error for 502 response")
	}
	var httpErr *HTTPError
	if !isHTTPError(err, &httpErr) {
		t.Fatalf("expected *HTTPError, got %T", err)
	}
}

func TestHTTPClient_FinalizeSampleRun_ServerError(t *testing.T) {
	c := NewHTTPClientWithClient("http://ah.test", &http.Client{
		Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: 500,
				Header:     http.Header{},
				Body:       io.NopCloser(strings.NewReader("")),
			}, nil
		}),
	})
	err := c.FinalizeSampleRun(context.Background(), FinalizeSampleRunRequest{SampleRunID: "s1"})
	if err == nil {
		t.Fatal("expected error for 500 response")
	}
}

func TestHTTPClient_EvaluateGC_ServerError(t *testing.T) {
	c := NewHTTPClientWithClient("http://ah.test", &http.Client{
		Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: 500,
				Header:     http.Header{},
				Body:       io.NopCloser(strings.NewReader("")),
			}, nil
		}),
	})
	err := c.EvaluateGC(context.Background(), EvaluateGCRequest{SampleRunID: "s1"})
	if err == nil {
		t.Fatal("expected error for 500 response")
	}
}

func TestHTTPClient_GetSampleRunLifecycle_ServerError(t *testing.T) {
	c := NewHTTPClientWithClient("http://ah.test", &http.Client{
		Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: 500,
				Header:     http.Header{},
				Body:       io.NopCloser(strings.NewReader("")),
			}, nil
		}),
	})
	_, _, err := c.GetSampleRunLifecycle(context.Background(), GetSampleRunLifecycleRequest{SampleRunID: "s1"})
	if err == nil {
		t.Fatal("expected error for 500 response")
	}
	var httpErr *HTTPError
	if !isHTTPError(err, &httpErr) {
		t.Fatalf("expected *HTTPError, got %T", err)
	}
}

func isHTTPError(err error, target **HTTPError) bool {
	if err == nil {
		return false
	}
	he, ok := err.(*HTTPError)
	if ok && target != nil {
		*target = he
	}
	return ok
}
