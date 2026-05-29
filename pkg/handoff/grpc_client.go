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
			ExpectedSize:   resp.GetMaterializationPlan().GetExpectedSizeBytes(),
			SourceLocation: grpcArtifactLocation(resp.GetMaterializationPlan().GetSourceLocation()),
			LocalPath:      resp.GetMaterializationPlan().GetLocalPath(),
		},
		MaterializationCandidates: grpcMaterializationCandidates(resp.GetMaterializationCandidates()),
		Reason:                    resp.GetReason(),
		Retryable:                 resp.GetRetryable(),
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
			LogicalUri:        req.LogicalURI,
			Locations:         grpcArtifactLocations(req.Locations),
		},
	}); err != nil {
		if c.metrics != nil {
			c.metrics.IncHandoffRegisterArtifactErrors()
		}
		return fmt.Errorf("handoff register artifact failed: %w", err)
	}
	return nil
}

func grpcArtifactLocations(locations []ArtifactLocation) []*ahv1.ArtifactLocation {
	if len(locations) == 0 {
		return nil
	}
	out := make([]*ahv1.ArtifactLocation, 0, len(locations))
	for _, location := range locations {
		if converted := grpcArtifactLocationFromClient(location); converted != nil {
			out = append(out, converted)
		}
	}
	return out
}

func grpcArtifactLocationFromClient(location ArtifactLocation) *ahv1.ArtifactLocation {
	switch {
	case location.NodeLocal != nil:
		return &ahv1.ArtifactLocation{
			Backend: &ahv1.ArtifactLocation_NodeLocal{
				NodeLocal: &ahv1.NodeLocalLocation{
					NodeName: location.NodeLocal.NodeName,
					Path:     location.NodeLocal.Path,
				},
			},
		}
	case location.HTTP != nil:
		return &ahv1.ArtifactLocation{
			Backend: &ahv1.ArtifactLocation_Http{
				Http: &ahv1.HttpSource{
					Uri: location.HTTP.URI,
				},
			},
		}
	default:
		return nil
	}
}

func grpcArtifactLocation(location *ahv1.ArtifactLocation) *ArtifactLocation {
	if location == nil {
		return nil
	}
	switch backend := location.GetBackend().(type) {
	case *ahv1.ArtifactLocation_NodeLocal:
		return &ArtifactLocation{
			NodeLocal: &NodeLocalLocation{
				NodeName: backend.NodeLocal.GetNodeName(),
				Path:     backend.NodeLocal.GetPath(),
			},
		}
	case *ahv1.ArtifactLocation_Http:
		return &ArtifactLocation{
			HTTP: &HTTPSource{
				URI: backend.Http.GetUri(),
			},
		}
	default:
		return nil
	}
}

func grpcMaterializationCandidates(candidates []*ahv1.MaterializationCandidate) []MaterializationCandidate {
	if len(candidates) == 0 {
		return nil
	}
	out := make([]MaterializationCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate == nil {
			continue
		}
		item := MaterializationCandidate{
			Priority:       int(candidate.GetPriority()),
			Mode:           candidate.GetMode(),
			SourceRef:      candidate.GetSourceRef(),
			ExpectedDigest: candidate.GetExpectedDigest(),
			ExpectedSize:   candidate.GetExpectedSizeBytes(),
			LocalPath:      candidate.GetLocalPath(),
			SourceLocation: grpcArtifactLocation(candidate.GetSourceLocation()),
			URI:            candidate.GetUri(),
		}
		if len(candidate.GetConditions()) != 0 {
			item.Conditions = make([]MaterializationCondition, 0, len(candidate.GetConditions()))
			for _, condition := range candidate.GetConditions() {
				item.Conditions = append(item.Conditions, MaterializationCondition{
					Kind:      condition.GetKind(),
					NodeName:  condition.GetNodeName(),
					BackendID: condition.GetBackendId(),
					SourceRef: condition.GetSourceRef(),
					State:     condition.GetState(),
				})
			}
		}
		out = append(out, item)
	}
	return out
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
