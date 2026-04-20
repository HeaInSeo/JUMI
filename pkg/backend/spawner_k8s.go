package backend

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/HeaInSeo/JUMI/pkg/spec"
	spimp "github.com/seoyhaein/spawner/cmd/imp"
	spapi "github.com/seoyhaein/spawner/pkg/api"
	spdriver "github.com/seoyhaein/spawner/pkg/driver"
)

type SpawnerK8sAdapter struct {
	driver   spdriver.Driver
	observer *spimp.K8sObserver
	bounded  *spimp.BoundedDriver
}

type spawnerHandle struct {
	inner     spdriver.Handle
	jobName   string
	queueName string
}

func NewSpawnerK8sAdapterFromKubeconfig(namespace, kubeconfigPath string, maxConcurrentRelease int) (*SpawnerK8sAdapter, error) {
	inner, err := spimp.NewK8sFromKubeconfig(namespace, kubeconfigPath)
	if err != nil {
		return nil, err
	}
	observer, err := spimp.NewK8sObserverFromKubeconfig(namespace, kubeconfigPath)
	if err != nil {
		return nil, err
	}
	var drv spdriver.Driver = inner
	var bounded *spimp.BoundedDriver
	if maxConcurrentRelease > 0 {
		bounded = spimp.NewBoundedDriver(inner, maxConcurrentRelease)
		drv = bounded
	}
	return &SpawnerK8sAdapter{driver: drv, observer: observer, bounded: bounded}, nil
}

func (a *SpawnerK8sAdapter) AdapterStatus() AdapterStatus {
	status := AdapterStatus{Ready: a.driver != nil}
	if a.bounded != nil {
		stats := a.bounded.Stats()
		status.ReleaseBounded = true
		status.ReleaseInflight = int(stats.Inflight)
		status.ReleaseSlotsAvailable = stats.Available
		status.ReleaseMaxConcurrent = stats.MaxConcurrent
	}
	return status
}

func (a *SpawnerK8sAdapter) PrepareNode(ctx context.Context, run spec.RunRecord, node spec.Node) (PreparedNode, error) {
	prepared, err := a.driver.Prepare(ctx, toSpawnerRunSpec(run, node))
	if err != nil {
		return nil, err
	}
	return prepared, nil
}

func (a *SpawnerK8sAdapter) StartNode(ctx context.Context, prepared PreparedNode) (Handle, error) {
	spPrepared, ok := prepared.(spdriver.Prepared)
	if !ok {
		return nil, fmt.Errorf("unexpected prepared type %T", prepared)
	}
	handle, err := a.driver.Start(ctx, spPrepared)
	if err != nil {
		return nil, err
	}
	jobName, _ := extractHandleJobName(handle)
	queueName := extractPreparedQueueName(prepared)
	return spawnerHandle{inner: handle, jobName: jobName, queueName: queueName}, nil
}

func (a *SpawnerK8sAdapter) ObserveNode(ctx context.Context, handle Handle) (*OptionalKueueInfo, error) {
	h, ok := handle.(spawnerHandle)
	if !ok {
		return nil, nil
	}
	if h.queueName == "" || h.jobName == "" || a.observer == nil {
		return nil, nil
	}
	info := &OptionalKueueInfo{Observed: false, QueueName: h.queueName}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		obs, err := a.observer.ObserveWorkload(ctx, h.jobName)
		if err == nil {
			info.Observed = true
			info.WorkloadName = obs.WorkloadName
			info.PendingReason = obs.PendingReason
			info.Admitted = obs.Admitted
			if obs.Admitted {
				if podObs, podErr := a.observer.ObservePod(ctx, h.jobName); podErr == nil {
					info.PodName = podObs.PodName
					info.Scheduled = podObs.Scheduled
					info.UnschedulableReason = podObs.UnschedulableReason
				}
			}
			return info, nil
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(250 * time.Millisecond):
		}
	}
	return info, nil
}

func (a *SpawnerK8sAdapter) WaitNode(ctx context.Context, handle Handle) (ExecutionResult, error) {
	h, ok := handle.(spawnerHandle)
	if !ok {
		return ExecutionResult{TerminalStopCause: "failed", TerminalFailureReason: "backend_wait_error"}, fmt.Errorf("unexpected handle type %T", handle)
	}
	event, err := a.driver.Wait(ctx, h.inner)
	if err != nil {
		if ctx.Err() != nil {
			return ExecutionResult{TerminalStopCause: "canceled", TerminalFailureReason: "cancellation_requested"}, ctx.Err()
		}
		return ExecutionResult{TerminalStopCause: "failed", TerminalFailureReason: "backend_wait_error"}, err
	}

	result := ExecutionResult{}
	switch event.State {
	case spapi.StateSucceeded:
		result.Succeeded = true
		result.TerminalStopCause = "finished"
	case spapi.StateCancelled:
		result.TerminalStopCause = "canceled"
		result.TerminalFailureReason = "cancellation_propagated"
		return result, fmt.Errorf("node canceled")
	case spapi.StateFailed:
		result.TerminalStopCause = "failed"
		result.TerminalFailureReason = "backend_wait_error"
		if strings.TrimSpace(event.Message) != "" {
			return result, fmt.Errorf("node failed: %s", event.Message)
		}
		return result, fmt.Errorf("node failed")
	default:
		result.TerminalStopCause = "failed"
		result.TerminalFailureReason = "backend_wait_error"
		return result, fmt.Errorf("unexpected backend terminal state: %s", event.State)
	}
	return result, nil
}

func (a *SpawnerK8sAdapter) CancelNode(ctx context.Context, handle Handle) error {
	h, ok := handle.(spawnerHandle)
	if !ok {
		return fmt.Errorf("unexpected handle type %T", handle)
	}
	return a.driver.Cancel(ctx, h.inner)
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

func extractHandleJobName(handle spdriver.Handle) (string, bool) {
	v := reflect.ValueOf(handle)
	if v.Kind() == reflect.Struct {
		f := v.FieldByName("name")
		if f.IsValid() && f.Kind() == reflect.String {
			return f.String(), true
		}
	}
	return "", false
}

func extractPreparedQueueName(prepared PreparedNode) string {
	v := reflect.ValueOf(prepared)
	if v.Kind() == reflect.Struct {
		jobField := v.FieldByName("job")
		if jobField.IsValid() && !jobField.IsNil() {
			labelsField := jobField.Elem().FieldByName("ObjectMeta").FieldByName("Labels")
			if labelsField.IsValid() && labelsField.Kind() == reflect.Map {
				iter := labelsField.MapRange()
				for iter.Next() {
					if iter.Key().String() == "kueue.x-k8s.io/queue-name" {
						return iter.Value().String()
					}
				}
			}
		}
	}
	return ""
}
