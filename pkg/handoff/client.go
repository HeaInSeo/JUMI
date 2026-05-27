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
	ProducerAttemptID  string `json:"producerAttemptId,omitempty"`
	ChildAttemptID     string `json:"childAttemptId,omitempty"`
	ProducerOutputName string `json:"producerOutputName"`
	ArtifactID         string `json:"artifactId,omitempty"`
	ConsumePolicy      string `json:"consumePolicy,omitempty"`
	ExpectedDigest     string `json:"expectedDigest,omitempty"`
	Required           bool   `json:"required,omitempty"`
	TargetNodeName     string `json:"targetNodeName,omitempty"`
}

type PlacementIntent struct {
	Mode     string `json:"mode,omitempty"`
	NodeName string `json:"nodeName,omitempty"`
}

type ArtifactLocation struct {
	NodeLocal *NodeLocalLocation `json:"nodeLocal,omitempty"`
	HTTP      *HTTPSource        `json:"http,omitempty"`
}

type NodeLocalLocation struct {
	NodeName string `json:"nodeName,omitempty"`
	Path     string `json:"path,omitempty"`
}

type HTTPSource struct {
	URI     string            `json:"uri,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
}

type MaterializationPlan struct {
	Mode           string            `json:"mode,omitempty"`
	URI            string            `json:"uri,omitempty"`
	ExpectedDigest string            `json:"expectedDigest,omitempty"`
	SourceLocation *ArtifactLocation `json:"sourceLocation,omitempty"`
	LocalPath      string            `json:"localPath,omitempty"`
}

type MaterializationCondition struct {
	Kind      string `json:"kind"`
	NodeName  string `json:"nodeName,omitempty"`
	BackendID string `json:"backendId,omitempty"`
	SourceRef string `json:"sourceRef,omitempty"`
	State     string `json:"state,omitempty"`
}

type MaterializationCandidate struct {
	Priority       int                        `json:"priority"`
	Mode           string                     `json:"mode,omitempty"`
	SourceRef      string                     `json:"sourceRef,omitempty"`
	ExpectedDigest string                     `json:"expectedDigest,omitempty"`
	LocalPath      string                     `json:"localPath,omitempty"`
	SourceLocation *ArtifactLocation          `json:"sourceLocation,omitempty"`
	URI            string                     `json:"uri,omitempty"`
	Conditions     []MaterializationCondition `json:"conditions,omitempty"`
}

type ResolveBindingResponse struct {
	ResolutionStatus          string                     `json:"resolutionStatus"`
	Decision                  string                     `json:"decision"`
	PlacementIntent           PlacementIntent            `json:"placementIntent"`
	MaterializationPlan       MaterializationPlan        `json:"materializationPlan"`
	MaterializationCandidates []MaterializationCandidate `json:"materializationCandidates,omitempty"`
	Reason                    string                     `json:"reason,omitempty"`
	Retryable                 bool                       `json:"retryable,omitempty"`

	// Legacy HTTP shim fields kept for backward-compatible decoding.
	SourceNodeName          string `json:"sourceNodeName,omitempty"`
	ArtifactURI             string `json:"artifactURI,omitempty"`
	RequiresMaterialization bool   `json:"requiresMaterialization,omitempty"`
}

type NotifyNodeTerminalRequest struct {
	SampleRunID   string `json:"sampleRunId"`
	NodeID        string `json:"nodeId"`
	AttemptID     string `json:"attemptId,omitempty"`
	TerminalState string `json:"terminalState"`
}

type FinalizeSampleRunRequest struct {
	SampleRunID string `json:"sampleRunId"`
}

type EvaluateGCRequest struct {
	SampleRunID string `json:"sampleRunId"`
}

type RegisterArtifactRequest struct {
	SampleRunID       string             `json:"sampleRunId"`
	ProducerNodeID    string             `json:"producerNodeId"`
	ProducerAttemptID string             `json:"producerAttemptId,omitempty"`
	OutputName        string             `json:"outputName"`
	ArtifactID        string             `json:"artifactId,omitempty"`
	Digest            string             `json:"digest,omitempty"`
	NodeName          string             `json:"nodeName,omitempty"`
	URI               string             `json:"uri,omitempty"`
	LogicalURI        string             `json:"logicalUri,omitempty"`
	Locations         []ArtifactLocation `json:"locations,omitempty"`
	SizeBytes         int64              `json:"sizeBytes,omitempty"`
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
	return normalizeResolveBindingResponse(ResolveBindingResponse{
		ResolutionStatus: "RESOLVED",
		Decision:         "noop",
		PlacementIntent: PlacementIntent{
			Mode:     "preferred_node",
			NodeName: firstNonEmpty(req.TargetNodeName, req.ProducerNodeID),
		},
		MaterializationPlan: MaterializationPlan{Mode: "none"},
	}), nil
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
	return NewHTTPClientWithClient(baseURL, &http.Client{Timeout: timeout})
}

func NewHTTPClientWithClient(baseURL string, client *http.Client) *HTTPClient {
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second}
	}
	return &HTTPClient{
		baseURL:    strings.TrimRight(baseURL, "/"),
		httpClient: client,
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
			ProducerAttemptID  string `json:"producerAttemptId,omitempty"`
			ChildAttemptID     string `json:"childAttemptId,omitempty"`
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
	body.Binding.ProducerAttemptID = req.ProducerAttemptID
	body.Binding.ChildAttemptID = req.ChildAttemptID
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
	defer func() {
		_ = resp.Body.Close()
	}()
	if resp.StatusCode >= 300 {
		return ResolveBindingResponse{}, fmt.Errorf("handoff resolve failed with status %d", resp.StatusCode)
	}
	var resolved ResolveBindingResponse
	if err := json.NewDecoder(resp.Body).Decode(&resolved); err != nil {
		return ResolveBindingResponse{}, err
	}
	return normalizeResolveBindingResponse(resolved), nil
}

func (c *HTTPClient) RegisterArtifact(ctx context.Context, req RegisterArtifactRequest) error {
	var body struct {
		Artifact struct {
			SampleRunID       string             `json:"sampleRunId"`
			ProducerNodeID    string             `json:"producerNodeId"`
			ProducerAttemptID string             `json:"producerAttemptId,omitempty"`
			OutputName        string             `json:"outputName"`
			ArtifactID        string             `json:"artifactId,omitempty"`
			Digest            string             `json:"digest,omitempty"`
			NodeName          string             `json:"nodeName,omitempty"`
			URI               string             `json:"uri,omitempty"`
			LogicalURI        string             `json:"logicalUri,omitempty"`
			Locations         []ArtifactLocation `json:"locations,omitempty"`
			SizeBytes         int64              `json:"sizeBytes,omitempty"`
		} `json:"artifact"`
	}
	body.Artifact.SampleRunID = req.SampleRunID
	body.Artifact.ProducerNodeID = req.ProducerNodeID
	body.Artifact.ProducerAttemptID = req.ProducerAttemptID
	body.Artifact.OutputName = req.OutputName
	body.Artifact.ArtifactID = req.ArtifactID
	body.Artifact.Digest = req.Digest
	body.Artifact.NodeName = req.NodeName
	body.Artifact.URI = req.URI
	body.Artifact.LogicalURI = req.LogicalURI
	body.Artifact.Locations = req.Locations
	body.Artifact.SizeBytes = req.SizeBytes
	payload, err := json.Marshal(body)
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
	defer func() {
		_ = resp.Body.Close()
	}()
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
	defer func() {
		_ = resp.Body.Close()
	}()
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
	defer func() {
		_ = resp.Body.Close()
	}()
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
	defer func() {
		_ = resp.Body.Close()
	}()
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

func normalizeResolveBindingResponse(resolved ResolveBindingResponse) ResolveBindingResponse {
	if resolved.PlacementIntent.NodeName == "" && resolved.SourceNodeName != "" {
		resolved.PlacementIntent.NodeName = resolved.SourceNodeName
	}
	if resolved.PlacementIntent.Mode == "" && resolved.PlacementIntent.NodeName != "" {
		resolved.PlacementIntent.Mode = "required_node"
	}
	if isZeroMaterializationPlan(resolved.MaterializationPlan) && len(resolved.MaterializationCandidates) > 0 {
		resolved.MaterializationPlan = planFromCandidate(resolved.MaterializationCandidates[0])
	}
	if resolved.MaterializationPlan.URI == "" && resolved.ArtifactURI != "" {
		resolved.MaterializationPlan.URI = resolved.ArtifactURI
	}
	if resolved.MaterializationPlan.Mode == "" {
		switch {
		case resolved.RequiresMaterialization:
			resolved.MaterializationPlan.Mode = "remote_fetch"
		case resolved.MaterializationPlan.URI != "":
			resolved.MaterializationPlan.Mode = "remote_fetch"
		default:
			resolved.MaterializationPlan.Mode = "none"
		}
	}
	return resolved
}

func isZeroMaterializationPlan(plan MaterializationPlan) bool {
	return strings.TrimSpace(plan.Mode) == "" &&
		strings.TrimSpace(plan.URI) == "" &&
		strings.TrimSpace(plan.ExpectedDigest) == "" &&
		plan.SourceLocation == nil &&
		strings.TrimSpace(plan.LocalPath) == ""
}

func planFromCandidate(candidate MaterializationCandidate) MaterializationPlan {
	return MaterializationPlan{
		Mode:           candidate.Mode,
		URI:            candidate.URI,
		ExpectedDigest: candidate.ExpectedDigest,
		SourceLocation: candidate.SourceLocation,
		LocalPath:      candidate.LocalPath,
	}
}
