package observe

import (
	"context"

	"github.com/HeaInSeo/JUMI/internal/util"
	"github.com/HeaInSeo/JUMI/pkg/registry"
	"github.com/HeaInSeo/JUMI/pkg/spec"
)

type BackendSnapshot struct {
	Ready                 bool
	ReleaseBounded        bool
	ReleaseInflight       int
	ReleaseSlotsAvailable int
	ReleaseMaxConcurrent  int
}

type StatusSnapshot struct {
	RunningRuns           int      `json:"runningRuns"`
	ReleaseWaitNodes      int      `json:"releaseWaitNodes"`
	BackendReady          bool     `json:"backendReady"`
	ReleaseBounded        bool     `json:"releaseBounded,omitempty"`
	ReleaseInflight       int      `json:"releaseInflight,omitempty"`
	ReleaseSlotsAvailable int      `json:"releaseSlotsAvailable,omitempty"`
	ReleaseMaxConcurrent  int      `json:"releaseMaxConcurrent,omitempty"`
	RecentErrors          []string `json:"recentErrors,omitempty"`
}

func SnapshotFromRegistry(ctx context.Context, reg registry.Registry, backend BackendSnapshot) (StatusSnapshot, error) {
	runs, err := reg.ListRuns(ctx)
	if err != nil {
		return StatusSnapshot{}, err
	}
	snapshot := StatusSnapshot{
		BackendReady:          backend.Ready,
		ReleaseBounded:        backend.ReleaseBounded,
		ReleaseInflight:       backend.ReleaseInflight,
		ReleaseSlotsAvailable: backend.ReleaseSlotsAvailable,
		ReleaseMaxConcurrent:  backend.ReleaseMaxConcurrent,
	}
	for _, run := range runs {
		active := run.Status == spec.RunStatusAdmitted || run.Status == spec.RunStatusRunning
		if active {
			snapshot.RunningRuns++
		}
		// Only query nodes and events for active runs to avoid O(N×M) queries
		// across all historical runs as the registry grows.
		if !active {
			continue
		}
		nodes, err := reg.ListNodes(ctx, run.RunID)
		if err != nil {
			return StatusSnapshot{}, err
		}
		for _, node := range nodes {
			if node.Status == spec.NodeStatusReady || node.Status == spec.NodeStatusReleasing {
				snapshot.ReleaseWaitNodes++
			}
		}
		events, err := reg.ListEvents(ctx, run.RunID, 5)
		if err != nil {
			return StatusSnapshot{}, err
		}
		for _, event := range events {
			if event.Level == "error" {
				snapshot.RecentErrors = append(snapshot.RecentErrors, event.Type+":"+util.FirstNonEmpty(event.FailureReason, event.Message))
			}
		}
	}
	return snapshot, nil
}
