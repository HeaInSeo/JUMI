package handoff

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type ResolveBindingRequest struct {
	RunID              string `json:"runId,omitempty"`
	SampleRunID        string `json:"sampleRunId,omitempty"`
	ChildNodeID        string `json:"childNodeId"`
	BindingName        string `json:"bindingName"`
	ChildInputName     string `json:"childInputName,omitempty"`
	ProducerNodeID     string `json:"producerNodeId"`
	ProducerOutputName string `json:"producerOutputName"`
	ArtifactID         string `json:"artifactId,omitempty"`
	ConsumePolicy      string `json:"consumePolicy,omitempty"`
	ExpectedDigest     string `json:"expectedDigest,omitempty"`
	Required           bool   `json:"required,omitempty"`
	TargetNodeName     string `json:"targetNodeName,omitempty"`
}

type ResolveBindingResponse struct {
	ResolutionStatus        string `json:"resolutionStatus"`
	Decision                string `json:"decision"`
	SourceNodeName          string `json:"sourceNodeName"`
	ArtifactURI             string `json:"artifactURI"`
	RequiresMaterialization bool   `json:"requiresMaterialization"`
}

type NotifyNodeTerminalRequest struct {
	SampleRunID   string `json:"sampleRunId"`
	NodeID        string `json:"nodeId"`
	TerminalState string `json:"terminalState"`
}

type FinalizeSampleRunRequest struct {
	SampleRunID string `json:"sampleRunId"`
}

type EvaluateGCRequest struct {
	SampleRunID string `json:"sampleRunId"`
}

type RegisterArtifactRequest struct {
	SampleRunID    string `json:"sampleRunId"`
	ProducerNodeID string `json:"producerNodeId"`
	OutputName     string `json:"outputName"`
	ArtifactID     string `json:"artifactId,omitempty"`
	Digest         string `json:"digest,omitempty"`
	NodeName       string `json:"nodeName,omitempty"`
	URI            string `json:"uri,omitempty"`
}

type Client interface {
	ResolveBinding(ctx context.Context, req ResolveBindingRequest) (ResolveBindingResponse, error)
	RegisterArtifact(ctx context.Context, req RegisterArtifactRequest) error
	NotifyNodeTerminal(ctx context.Context, req NotifyNodeTerminalRequest) error
	FinalizeSampleRun(ctx context.Context, req FinalizeSampleRunRequest) error
	EvaluateGC(ctx context.Context, req EvaluateGCRequest) error
}

type NoopClient struct{}

func NewNoopClient() *NoopClient {
	return &NoopClient{}
}

func (c *NoopClient) ResolveBinding(_ context.Context, req ResolveBindingRequest) (ResolveBindingResponse, error) {
	return ResolveBindingResponse{
		ResolutionStatus:        "RESOLVED",
		Decision:                "noop",
		SourceNodeName:          firstNonEmpty(req.TargetNodeName, req.ProducerNodeID),
		RequiresMaterialization: false,
	}, nil
}

func (c *NoopClient) RegisterArtifact(_ context.Context, _ RegisterArtifactRequest) error {
	return nil
}

func (c *NoopClient) NotifyNodeTerminal(_ context.Context, _ NotifyNodeTerminalRequest) error {
	return nil
}

func (c *NoopClient) FinalizeSampleRun(_ context.Context, _ FinalizeSampleRunRequest) error {
	return nil
}

func (c *NoopClient) EvaluateGC(_ context.Context, _ EvaluateGCRequest) error {
	return nil
}

type HTTPClient struct {
	baseURL    string
	httpClient *http.Client
}

func NewHTTPClient(baseURL string, timeout time.Duration) *HTTPClient {
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	return &HTTPClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		httpClient: &http.Client{
			Timeout: timeout,
		},
	}
}

func (c *HTTPClient) ResolveBinding(ctx context.Context, req ResolveBindingRequest) (ResolveBindingResponse, error) {
	body := struct {
		Binding struct {
			BindingName        string `json:"bindingName"`
			SampleRunID        string `json:"sampleRunId"`
			ChildNodeID        string `json:"childNodeId"`
			ChildInputName     string `json:"childInputName,omitempty"`
			ProducerNodeID     string `json:"producerNodeId"`
			ProducerOutputName string `json:"producerOutputName"`
			ArtifactID         string `json:"artifactId,omitempty"`
			ConsumePolicy      string `json:"consumePolicy,omitempty"`
			ExpectedDigest     string `json:"expectedDigest,omitempty"`
			Required           bool   `json:"required,omitempty"`
		} `json:"binding"`
		TargetNodeName string `json:"targetNodeName"`
	}{TargetNodeName: req.TargetNodeName}
	body.Binding.BindingName = req.BindingName
	body.Binding.SampleRunID = req.SampleRunID
	body.Binding.ChildNodeID = req.ChildNodeID
	body.Binding.ChildInputName = req.ChildInputName
	body.Binding.ProducerNodeID = req.ProducerNodeID
	body.Binding.ProducerOutputName = req.ProducerOutputName
	body.Binding.ArtifactID = req.ArtifactID
	body.Binding.ConsumePolicy = req.ConsumePolicy
	body.Binding.ExpectedDigest = req.ExpectedDigest
	body.Binding.Required = req.Required
	payload, err := json.Marshal(body)
	if err != nil {
		return ResolveBindingResponse{}, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/handoffs:resolve", bytes.NewReader(payload))
	if err != nil {
		return ResolveBindingResponse{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return ResolveBindingResponse{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return ResolveBindingResponse{}, fmt.Errorf("handoff resolve failed with status %d", resp.StatusCode)
	}
	var resolved ResolveBindingResponse
	if err := json.NewDecoder(resp.Body).Decode(&resolved); err != nil {
		return ResolveBindingResponse{}, err
	}
	return resolved, nil
}

func (c *HTTPClient) RegisterArtifact(ctx context.Context, req RegisterArtifactRequest) error {
	payload, err := json.Marshal(struct {
		SampleRunID    string `json:"sampleRunId"`
		ProducerNodeID string `json:"producerNodeId"`
		OutputName     string `json:"outputName"`
		ArtifactID     string `json:"artifactId,omitempty"`
		Digest         string `json:"digest,omitempty"`
		NodeName       string `json:"nodeName,omitempty"`
		URI            string `json:"uri,omitempty"`
	}{
		SampleRunID:    req.SampleRunID,
		ProducerNodeID: req.ProducerNodeID,
		OutputName:     req.OutputName,
		ArtifactID:     req.ArtifactID,
		Digest:         req.Digest,
		NodeName:       req.NodeName,
		URI:            req.URI,
	})
	if err != nil {
		return err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/artifacts:register", bytes.NewReader(payload))
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("handoff register artifact failed with status %d", resp.StatusCode)
	}
	return nil
}

func (c *HTTPClient) NotifyNodeTerminal(ctx context.Context, req NotifyNodeTerminalRequest) error {
	payload, err := json.Marshal(req)
	if err != nil {
		return err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/nodes:notifyTerminal", bytes.NewReader(payload))
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("handoff notify node terminal failed with status %d", resp.StatusCode)
	}
	return nil
}

func (c *HTTPClient) FinalizeSampleRun(ctx context.Context, req FinalizeSampleRunRequest) error {
	payload, err := json.Marshal(req)
	if err != nil {
		return err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/sampleRuns:finalize", bytes.NewReader(payload))
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("handoff finalize sample run failed with status %d", resp.StatusCode)
	}
	return nil
}

func (c *HTTPClient) EvaluateGC(ctx context.Context, req EvaluateGCRequest) error {
	payload, err := json.Marshal(req)
	if err != nil {
		return err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/sampleRuns:evaluateGC", bytes.NewReader(payload))
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("handoff evaluate gc failed with status %d", resp.StatusCode)
	}
	return nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
