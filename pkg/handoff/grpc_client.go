package handoff

import (
	"context"
	"fmt"

	ahv1 "github.com/HeaInSeo/JUMI/pkg/handoff/ahv1"
	"github.com/HeaInSeo/JUMI/pkg/metrics"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type GRPCClient struct {
	conn    *grpc.ClientConn
	client  ahv1.ArtifactHandoffResolverClient
	metrics *metrics.Metrics
}

func (c *GRPCClient) SetMetrics(m *metrics.Metrics) {
	c.metrics = m
}

func NewGRPCClient(target string) (*GRPCClient, error) {
	conn, err := grpc.NewClient(target,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return nil, err
	}
	return &GRPCClient{
		conn:   conn,
		client: ahv1.NewArtifactHandoffResolverClient(conn),
	}, nil
}

func (c *GRPCClient) Close() error {
	if c == nil || c.conn == nil {
		return nil
	}
	return c.conn.Close()
}

func (c *GRPCClient) ResolveBinding(ctx context.Context, req ResolveBindingRequest) (ResolveBindingResponse, error) {
	if c.metrics != nil {
		c.metrics.IncHandoffResolve()
	}
	resp, err := c.client.ResolveHandoff(ctx, &ahv1.ResolveHandoffRequest{
		Binding: &ahv1.ArtifactBinding{
			BindingName:        req.BindingName,
			SampleRunId:        req.SampleRunID,
			ChildNodeId:        req.ChildNodeID,
			ChildInputName:     req.ChildInputName,
			ProducerNodeId:     req.ProducerNodeID,
			ProducerAttemptId:  req.ProducerAttemptID,
			ChildAttemptId:     req.ChildAttemptID,
			ProducerOutputName: req.ProducerOutputName,
			ArtifactId:         req.ArtifactID,
			ConsumePolicy:      req.ConsumePolicy,
			ExpectedDigest:     req.ExpectedDigest,
			Required:           req.Required,
		},
		TargetNodeName: req.TargetNodeName,
	})
	if err != nil {
		if c.metrics != nil {
			c.metrics.IncHandoffResolveErrors()
		}
		return ResolveBindingResponse{}, err
	}
	return normalizeResolveBindingResponse(ResolveBindingResponse{
		ResolutionStatus: resp.GetResolutionStatus(),
		Decision:         resp.GetDecision(),
		PlacementIntent: PlacementIntent{
			Mode:     resp.GetPlacementIntent().GetMode(),
			NodeName: resp.GetPlacementIntent().GetNodeName(),
		},
		MaterializationPlan: MaterializationPlan{
			Mode:           resp.GetMaterializationPlan().GetMode(),
			URI:            resp.GetMaterializationPlan().GetUri(),
			ExpectedDigest: resp.GetMaterializationPlan().GetExpectedDigest(),
		},
		Reason:    resp.GetReason(),
		Retryable: resp.GetRetryable(),
	}), nil
}

func (c *GRPCClient) RegisterArtifact(ctx context.Context, req RegisterArtifactRequest) error {
	if c.metrics != nil {
		c.metrics.IncHandoffRegisterArtifact()
	}
	if _, err := c.client.RegisterArtifact(ctx, &ahv1.RegisterArtifactRequest{
		Artifact: &ahv1.ArtifactRef{
			SampleRunId:       req.SampleRunID,
			ProducerNodeId:    req.ProducerNodeID,
			ProducerAttemptId: req.ProducerAttemptID,
			OutputName:        req.OutputName,
			ArtifactId:        req.ArtifactID,
			Digest:            req.Digest,
			NodeName:          req.NodeName,
			Uri:               req.URI,
			SizeBytes:         req.SizeBytes,
		},
	}); err != nil {
		if c.metrics != nil {
			c.metrics.IncHandoffRegisterArtifactErrors()
		}
		return fmt.Errorf("handoff register artifact failed: %w", err)
	}
	return nil
}

func (c *GRPCClient) NotifyNodeTerminal(ctx context.Context, req NotifyNodeTerminalRequest) error {
	if c.metrics != nil {
		c.metrics.IncHandoffNotifyTerminal()
	}
	if _, err := c.client.NotifyNodeTerminal(ctx, &ahv1.NotifyNodeTerminalRequest{
		SampleRunId:   req.SampleRunID,
		NodeId:        req.NodeID,
		AttemptId:     req.AttemptID,
		TerminalState: req.TerminalState,
	}); err != nil {
		return fmt.Errorf("handoff notify node terminal failed: %w", err)
	}
	return nil
}

func (c *GRPCClient) FinalizeSampleRun(ctx context.Context, req FinalizeSampleRunRequest) error {
	if c.metrics != nil {
		c.metrics.IncHandoffFinalize()
	}
	if _, err := c.client.FinalizeSampleRun(ctx, &ahv1.FinalizeSampleRunRequest{
		SampleRunId: req.SampleRunID,
	}); err != nil {
		return fmt.Errorf("handoff finalize sample run failed: %w", err)
	}
	return nil
}

func (c *GRPCClient) EvaluateGC(ctx context.Context, req EvaluateGCRequest) error {
	if c.metrics != nil {
		c.metrics.IncHandoffGCEvaluate()
	}
	if _, err := c.client.EvaluateGC(ctx, &ahv1.EvaluateGCRequest{
		SampleRunId: req.SampleRunID,
	}); err != nil {
		return fmt.Errorf("handoff evaluate gc failed: %w", err)
	}
	return nil
}
