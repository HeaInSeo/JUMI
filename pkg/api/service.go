package api

import (
	"context"
	"errors"
	"time"

	"github.com/HeaInSeo/JUMI/pkg/executor"
	"github.com/HeaInSeo/JUMI/pkg/registry"
	"github.com/HeaInSeo/JUMI/pkg/spec"
)

type Service struct {
	registry registry.Registry
	engine   executor.Engine
}

func NewService(reg registry.Registry, eng executor.Engine) *Service {
	return &Service{registry: reg, engine: eng}
}

type SubmitRunRequest struct {
	Spec spec.ExecutableRunSpec `json:"spec"`
}

type SubmitRunResponse struct {
	RunID      string         `json:"runId"`
	Status     spec.RunStatus `json:"status"`
	AcceptedAt time.Time      `json:"acceptedAt"`
}

type GetRunRequest struct {
	RunID string `json:"runId"`
}

type GetRunResponse struct {
	Run spec.RunRecord `json:"run"`
}

type ListRunNodesRequest struct {
	RunID string `json:"runId"`
}

type ListRunNodesResponse struct {
	Nodes []spec.NodeRecord `json:"nodes"`
}

type ListNodeAttemptsRequest struct {
	RunID  string `json:"runId"`
	NodeID string `json:"nodeId"`
}

type ListNodeAttemptsResponse struct {
	Attempts []spec.AttemptRecord `json:"attempts"`
}

type ListRunEventsRequest struct {
	RunID string `json:"runId"`
	Limit int    `json:"limit,omitempty"`
}

type ListRunEventsResponse struct {
	Events []spec.EventRecord `json:"events"`
}

type CancelRunRequest struct {
	RunID  string `json:"runId"`
	Reason string `json:"reason,omitempty"`
}

type CancelRunResponse struct {
	RunID      string         `json:"runId"`
	Accepted   bool           `json:"accepted"`
	Status     spec.RunStatus `json:"status"`
	AcceptedAt time.Time      `json:"acceptedAt"`
}

func (s *Service) SubmitRun(ctx context.Context, req SubmitRunRequest) (SubmitRunResponse, error) {
	if err := spec.ValidateExecutableRunSpec(req.Spec); err != nil {
		return SubmitRunResponse{}, err
	}
	now := time.Now().UTC()
	record := spec.RunRecord{
		RunID:      req.Spec.Run.RunID,
		Status:     spec.RunStatusAccepted,
		AcceptedAt: now,
		Metadata:   req.Spec.Metadata,
		Spec:       req.Spec,
		Counters: spec.RunCounters{
			TotalNodes: len(req.Spec.Graph.Nodes),
		},
	}
	nodes := make([]spec.NodeRecord, 0, len(req.Spec.Graph.Nodes))
	for _, node := range req.Spec.Graph.Nodes {
		nodes = append(nodes, spec.NodeRecord{
			RunID:  req.Spec.Run.RunID,
			NodeID: node.NodeID,
			Status: spec.NodeStatusPending,
		})
	}
	if err := s.registry.CreateRun(ctx, record, nodes); err != nil {
		return SubmitRunResponse{}, err
	}
	if err := s.engine.Admit(ctx, record); err != nil {
		return SubmitRunResponse{}, err
	}
	return SubmitRunResponse{RunID: record.RunID, Status: spec.RunStatusAccepted, AcceptedAt: now}, nil
}

func (s *Service) GetRun(ctx context.Context, req GetRunRequest) (GetRunResponse, error) {
	record, err := s.registry.GetRun(ctx, req.RunID)
	if err != nil {
		return GetRunResponse{}, err
	}
	return GetRunResponse{Run: record}, nil
}

func (s *Service) ListRunNodes(ctx context.Context, req ListRunNodesRequest) (ListRunNodesResponse, error) {
	nodes, err := s.registry.ListNodes(ctx, req.RunID)
	if err != nil {
		return ListRunNodesResponse{}, err
	}
	return ListRunNodesResponse{Nodes: nodes}, nil
}

func (s *Service) ListNodeAttempts(ctx context.Context, req ListNodeAttemptsRequest) (ListNodeAttemptsResponse, error) {
	if req.RunID == "" {
		return ListNodeAttemptsResponse{}, errors.New("runId is required")
	}
	if req.NodeID == "" {
		return ListNodeAttemptsResponse{}, errors.New("nodeId is required")
	}
	attempts, err := s.registry.ListAttempts(ctx, req.RunID, req.NodeID)
	if err != nil {
		return ListNodeAttemptsResponse{}, err
	}
	return ListNodeAttemptsResponse{Attempts: attempts}, nil
}

func (s *Service) ListRunEvents(ctx context.Context, req ListRunEventsRequest) (ListRunEventsResponse, error) {
	if req.RunID == "" {
		return ListRunEventsResponse{}, errors.New("runId is required")
	}
	events, err := s.registry.ListEvents(ctx, req.RunID, req.Limit)
	if err != nil {
		return ListRunEventsResponse{}, err
	}
	return ListRunEventsResponse{Events: events}, nil
}

func (s *Service) CancelRun(ctx context.Context, req CancelRunRequest) (CancelRunResponse, error) {
	if req.RunID == "" {
		return CancelRunResponse{}, errors.New("runId is required")
	}
	if err := s.engine.Cancel(ctx, req.RunID, req.Reason); err != nil {
		return CancelRunResponse{}, err
	}
	record, err := s.registry.GetRun(ctx, req.RunID)
	if err != nil {
		return CancelRunResponse{}, err
	}
	return CancelRunResponse{
		RunID:      req.RunID,
		Accepted:   true,
		Status:     record.Status,
		AcceptedAt: time.Now().UTC(),
	}, nil
}
