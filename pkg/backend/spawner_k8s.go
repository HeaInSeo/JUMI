package backend

import (
	"context"
	"fmt"
	"strings"

	"github.com/HeaInSeo/JUMI/pkg/spec"
	spimp "github.com/seoyhaein/spawner/cmd/imp"
	spapi "github.com/seoyhaein/spawner/pkg/api"
	spdriver "github.com/seoyhaein/spawner/pkg/driver"
)

type SpawnerK8sAdapter struct {
	driver spdriver.Driver
}

func NewSpawnerK8sAdapterFromKubeconfig(namespace, kubeconfigPath string, maxConcurrentRelease int) (*SpawnerK8sAdapter, error) {
	inner, err := spimp.NewK8sFromKubeconfig(namespace, kubeconfigPath)
	if err != nil {
		return nil, err
	}
	var drv spdriver.Driver = inner
	if maxConcurrentRelease > 0 {
		drv = spimp.NewBoundedDriver(inner, maxConcurrentRelease)
	}
	return &SpawnerK8sAdapter{driver: drv}, nil
}

func (a *SpawnerK8sAdapter) ExecuteNode(ctx context.Context, run spec.RunRecord, node spec.Node) (ExecutionResult, error) {
	runSpec := toSpawnerRunSpec(run, node)
	prepared, err := a.driver.Prepare(ctx, runSpec)
	if err != nil {
		return ExecutionResult{
			TerminalStopCause:     "failed",
			TerminalFailureReason: "backend_prepare_error",
		}, err
	}
	handle, err := a.driver.Start(ctx, prepared)
	if err != nil {
		return ExecutionResult{
			TerminalStopCause:     "failed",
			TerminalFailureReason: "backend_start_error",
		}, err
	}
	event, err := a.driver.Wait(ctx, handle)
	if err != nil {
		return ExecutionResult{
			TerminalStopCause:     "failed",
			TerminalFailureReason: "backend_wait_error",
		}, err
	}

	result := ExecutionResult{}
	switch event.State {
	case spapi.StateSucceeded:
		result.Succeeded = true
		result.TerminalStopCause = "finished"
	case spapi.StateCancelled:
		result.TerminalStopCause = "canceled"
		result.TerminalFailureReason = "cancellation_propagated"
		return result, fmt.Errorf("node canceled: %s", node.NodeID)
	case spapi.StateFailed:
		result.TerminalStopCause = "failed"
		result.TerminalFailureReason = "backend_wait_error"
		if strings.TrimSpace(event.Message) != "" {
			return result, fmt.Errorf("node failed: %s", event.Message)
		}
		return result, fmt.Errorf("node failed: %s", node.NodeID)
	default:
		result.TerminalStopCause = "failed"
		result.TerminalFailureReason = "backend_wait_error"
		return result, fmt.Errorf("unexpected backend terminal state: %s", event.State)
	}
	return result, nil
}

func toSpawnerRunSpec(run spec.RunRecord, node spec.Node) spapi.RunSpec {
	labels := map[string]string{
		"jumi/run-id":  run.RunID,
		"jumi/node-id": node.NodeID,
	}
	if node.Kueue != nil && node.Kueue.QueueName != "" {
		labels["kueue.x-k8s.io/queue-name"] = node.Kueue.QueueName
		for k, v := range node.Kueue.Labels {
			labels[k] = v
		}
	}
	annotations := map[string]string{}
	for k, v := range node.Metadata {
		annotations[k] = v
	}
	if traceID := run.Spec.Run.TraceID; traceID != "" {
		annotations["jumi.trace-id"] = traceID
	}
	mounts := make([]spapi.Mount, 0, len(node.Mounts))
	for _, m := range node.Mounts {
		mounts = append(mounts, spapi.Mount{
			Source:   m.Source,
			Target:   m.Target,
			ReadOnly: strings.EqualFold(m.Mode, "ro") || strings.EqualFold(m.Mode, "readonly"),
		})
	}
	command := append(append([]string{}, node.Command...), node.Args...)
	return spapi.RunSpec{
		SpecVersion: 1,
		RunID:       fmt.Sprintf("%s-%s", run.RunID, node.NodeID),
		ImageRef:    node.Image,
		Command:     command,
		Env:         node.Env,
		Labels:      labels,
		Annotations: annotations,
		Mounts:      mounts,
		Resources: spapi.Resources{
			CPU:    node.ResourceProfile.CPU,
			Memory: node.ResourceProfile.Memory,
		},
		CorrelationID: run.Spec.Run.TraceID,
		Cleanup:       spapi.CleanupPolicy{TTLSecondsAfterFinished: 600},
	}
}
