package observe

import (
	"context"

	"github.com/HeaInSeo/JUMI/pkg/registry"
	"github.com/HeaInSeo/JUMI/pkg/spec"
)

type StatusSnapshot struct {
	RunningRuns      int      `json:"runningRuns"`
	ReleaseWaitNodes int      `json:"releaseWaitNodes"`
	BackendReady     bool     `json:"backendReady"`
	RecentErrors     []string `json:"recentErrors,omitempty"`
}

func SnapshotFromRegistry(ctx context.Context, reg registry.Registry, backendReady bool) (StatusSnapshot, error) {
	runs, err := reg.ListRuns(ctx)
	if err != nil {
		return StatusSnapshot{}, err
	}
	snapshot := StatusSnapshot{BackendReady: backendReady}
	for _, run := range runs {
		switch run.Status {
		case spec.RunStatusAdmitted, spec.RunStatusRunning:
			snapshot.RunningRuns++
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
				snapshot.RecentErrors = append(snapshot.RecentErrors, event.Type+":"+firstNonEmpty(event.FailureReason, event.Message))
			}
		}
	}
	return snapshot, nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
