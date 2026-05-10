package backend

import (
	"bytes"
	"context"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/HeaInSeo/JUMI/pkg/provenance"
	"github.com/HeaInSeo/JUMI/pkg/spec"
	spimp "github.com/seoyhaein/spawner/cmd/imp"
	spapi "github.com/seoyhaein/spawner/pkg/api"
	spdriver "github.com/seoyhaein/spawner/pkg/driver"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/remotecommand"
)

const outputManifestModeMetadataKey = "jumi.outputManifestMode"
const outputManifestModeWrappedShell = "wrapped-shell"
const outputManifestModeRuntimeHelper = "runtime-helper"

type SpawnerK8sAdapter struct {
	driver     spdriver.Driver
	observer   *spimp.K8sObserver
	bounded    *spimp.BoundedDriver
	ns         string
	clientset  kubernetes.Interface
	restConfig *rest.Config
}

type spawnerHandle struct {
	inner     spdriver.Handle
	jobName   string
	queueName string
}

func NewSpawnerK8sAdapterFromKubeconfig(namespace, kubeconfigPath string, maxConcurrentRelease int) (*SpawnerK8sAdapter, error) {
	cfg, err := buildK8sConfig(kubeconfigPath)
	if err != nil {
		return nil, fmt.Errorf("k8s config: %w", err)
	}
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
	cs, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("k8s clientset: %w", err)
	}
	return &SpawnerK8sAdapter{
		driver:     drv,
		observer:   observer,
		bounded:    bounded,
		ns:         namespace,
		clientset:  cs,
		restConfig: cfg,
	}, nil
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

func (a *SpawnerK8sAdapter) CollectOutputMetadata(ctx context.Context, handle Handle, node spec.Node) (map[string]OutputMetadata, error) {
	h, ok := handle.(spawnerHandle)
	if !ok {
		return nil, ErrOutputMetadataUnavailable
	}
	if h.jobName == "" || a.clientset == nil || a.restConfig == nil {
		return nil, ErrOutputMetadataUnavailable
	}
	pod, err := a.findJobPod(ctx, h.jobName)
	if err != nil {
		return nil, ErrOutputMetadataUnavailable
	}
	raw, err := readArtifactsManifestFromTerminationMessage(pod)
	if err != nil {
		return nil, fmt.Errorf("read artifacts manifest from pod termination message %s: %w", pod.Name, err)
	}
	if len(raw) == 0 {
		raw, err = a.readArtifactsManifest(ctx, pod.Name)
		if err != nil {
			return nil, ErrOutputMetadataUnavailable
		}
	}
	manifest, err := provenance.ParseArtifactManifest(raw)
	if err != nil {
		return nil, fmt.Errorf("parse artifacts manifest for pod %s: %w", pod.Name, err)
	}
	out := make(map[string]OutputMetadata, len(manifest.Artifacts))
	for _, outputName := range node.Outputs {
		record, ok := manifest.ByOutputName(outputName)
		if !ok {
			continue
		}
		out[outputName] = OutputMetadata{
			OutputName: outputName,
			URI:        record.URI,
			Digest:     record.Digest,
			SizeBytes:  record.SizeBytes,
		}
	}
	if len(out) == 0 {
		return nil, ErrOutputMetadataUnavailable
	}
	return out, nil
}

func (a *SpawnerK8sAdapter) findJobPod(ctx context.Context, jobName string) (*corev1.Pod, error) {
	pods, err := a.clientset.CoreV1().Pods(a.ns).List(ctx, metav1.ListOptions{
		LabelSelector: fmt.Sprintf("job-name=%s", jobName),
	})
	if err != nil {
		return nil, err
	}
	if len(pods.Items) == 0 {
		return nil, fmt.Errorf("no pods found for job %s", jobName)
	}
	return &pods.Items[0], nil
}

func readArtifactsManifestFromTerminationMessage(pod *corev1.Pod) ([]byte, error) {
	if pod == nil {
		return nil, nil
	}
	for _, status := range pod.Status.ContainerStatuses {
		if status.Name != "main" {
			continue
		}
		if raw := strings.TrimSpace(terminatedMessage(status.State.Terminated)); raw != "" {
			return []byte(raw), nil
		}
		if raw := strings.TrimSpace(terminatedMessage(status.LastTerminationState.Terminated)); raw != "" {
			return []byte(raw), nil
		}
	}
	return nil, nil
}

func terminatedMessage(state *corev1.ContainerStateTerminated) string {
	if state == nil {
		return ""
	}
	return state.Message
}

func (a *SpawnerK8sAdapter) readArtifactsManifest(ctx context.Context, podName string) ([]byte, error) {
	req := a.clientset.CoreV1().RESTClient().Post().
		Namespace(a.ns).
		Resource("pods").
		Name(podName).
		SubResource("exec")
	req.VersionedParams(&corev1.PodExecOptions{
		Container: "main",
		Command:   []string{"cat", provenance.DefaultArtifactsManifestPath},
		Stdout:    true,
		Stderr:    true,
	}, scheme.ParameterCodec)
	exec, err := remotecommand.NewSPDYExecutor(a.restConfig, "POST", req.URL())
	if err != nil {
		return nil, err
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if err := exec.StreamWithContext(ctx, remotecommand.StreamOptions{
		Stdout: &stdout,
		Stderr: &stderr,
	}); err != nil {
		if stderr.Len() > 0 {
			return nil, fmt.Errorf("%w: %s", err, strings.TrimSpace(stderr.String()))
		}
		return nil, err
	}
	return stdout.Bytes(), nil
}

func buildK8sConfig(kubeconfigPath string) (*rest.Config, error) {
	if kubeconfigPath != "" {
		return clientcmd.BuildConfigFromFlags("", kubeconfigPath)
	}
	if cfg, err := rest.InClusterConfig(); err == nil {
		return cfg, nil
	}
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	clientConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		loadingRules,
		&clientcmd.ConfigOverrides{},
	)
	return clientConfig.ClientConfig()
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
	env := cloneEnv(node.Env)
	injectOutputContractEnv(env, run, node)
	command = wrapCommandForManifestExport(command, node)
	runSpec := spapi.RunSpec{
		SpecVersion: 1,
		RunID:       fmt.Sprintf("%s-%s", run.RunID, node.NodeID),
		ImageRef:    node.Image,
		Command:     command,
		Env:         env,
		Labels:      labels,
		Annotations: annotations,
		Mounts:      mounts,
		Resources: spapi.Resources{
			CPU:    node.ResourceProfile.CPU,
			Memory: node.ResourceProfile.Memory,
		},
		CorrelationID: run.Spec.Run.TraceID,
		Cleanup:       spapi.CleanupPolicy{TTLSecondsAfterFinished: cleanupTTL(node)},
		Placement:     buildPlacement(node),
	}
	applyOptionalSpawnerFields(&runSpec, node)
	return runSpec
}

func cloneEnv(src map[string]string) map[string]string {
	if len(src) == 0 {
		return map[string]string{}
	}
	out := make(map[string]string, len(src))
	for k, v := range src {
		out[k] = v
	}
	return out
}

func injectOutputContractEnv(env map[string]string, run spec.RunRecord, node spec.Node) {
	if len(node.Outputs) == 0 {
		return
	}
	outputs := make([]string, 0, len(node.Outputs))
	for _, output := range node.Outputs {
		if output == "" {
			continue
		}
		outputs = append(outputs, output)
	}
	if len(outputs) == 0 {
		return
	}
	sort.Strings(outputs)
	env["JUMI_OUTPUT_MANIFEST_ENABLED"] = "true"
	env["JUMI_OUTPUT_MANIFEST_PATH"] = provenance.DefaultArtifactsManifestPath
	env["JUMI_OUTPUT_NAMES"] = strings.Join(outputs, ",")
	env["JUMI_RUN_ID"] = run.RunID
	env["JUMI_NODE_ID"] = node.NodeID
	if sampleRunID := run.Spec.Run.SampleRunID; sampleRunID != "" {
		env["JUMI_SAMPLE_RUN_ID"] = sampleRunID
	}
}

func wrapCommandForManifestExport(command []string, node spec.Node) []string {
	mode := manifestExportMode(node)
	if mode == "" || len(command) == 0 {
		return command
	}
	if mode == outputManifestModeRuntimeHelper {
		wrapped := []string{"/usr/local/bin/jumi-output-helper"}
		wrapped = append(wrapped, command...)
		return wrapped
	}
	script := `
"$@"
status=$?
if [ "$status" -ne 0 ]; then
  exit "$status"
fi
manifest_path="${JUMI_OUTPUT_MANIFEST_PATH}"
manifest_dir="${manifest_path%/*}"
mkdir -p "$manifest_dir"
tmp_path="${manifest_path}.tmp"
printf '{"artifacts":[' > "$tmp_path"
first=1
OLDIFS="$IFS"
IFS=','
for output in ${JUMI_OUTPUT_NAMES}; do
  path="/out/${output}"
  if [ ! -f "$path" ]; then
    continue
  fi
  uri="jumi://runs/${JUMI_RUN_ID}/nodes/${JUMI_NODE_ID}/outputs/${output}"
  size="$(wc -c < "$path" | tr -d '[:space:]')"
  digest=""
  if command -v sha256sum >/dev/null 2>&1; then
    digest="sha256:$(sha256sum "$path" | awk '{print $1}')"
  fi
  if [ "$first" -eq 0 ]; then
    printf ',' >> "$tmp_path"
  fi
  first=0
  printf '{"outputName":"%s","uri":"%s","digest":"%s","sizeBytes":%s}' "$output" "$uri" "$digest" "$size" >> "$tmp_path"
done
IFS="$OLDIFS"
printf ']}\n' >> "$tmp_path"
		mv "$tmp_path" "$manifest_path"
		if [ -w /dev/termination-log ]; then
		  cat "$manifest_path" > /dev/termination-log 2>/dev/null || true
		fi
	`
	wrapped := []string{"/bin/sh", "-ceu", script, "jumi-output-manifest"}
	wrapped = append(wrapped, command...)
	return wrapped
}

func manifestExportMode(node spec.Node) string {
	if len(node.Outputs) == 0 || node.Metadata == nil {
		return ""
	}
	switch node.Metadata[outputManifestModeMetadataKey] {
	case outputManifestModeWrappedShell, outputManifestModeRuntimeHelper:
		return node.Metadata[outputManifestModeMetadataKey]
	default:
		return ""
	}
}

func applyOptionalSpawnerFields(target *spapi.RunSpec, node spec.Node) {
	if target == nil {
		return
	}
	setOptionalStringField(target, "WorkingDir", node.WorkingDir)
	setOptionalStringField(target, "ServiceAccountName", node.ServiceAccountName)
}

func cleanupTTL(node spec.Node) int32 {
	if node.CleanupPolicy.TTLSecondsAfterFinished > 0 {
		return node.CleanupPolicy.TTLSecondsAfterFinished
	}
	return 600
}

func buildPlacement(node spec.Node) *spapi.Placement {
	if node.Placement == nil || len(node.Placement.NodeSelector) == 0 {
		return nil
	}
	out := make(map[string]string, len(node.Placement.NodeSelector))
	for k, v := range node.Placement.NodeSelector {
		out[k] = v
	}
	return &spapi.Placement{NodeSelector: out}
}

func setOptionalStringField(target *spapi.RunSpec, fieldName string, value string) {
	if target == nil || value == "" {
		return
	}
	v := reflect.ValueOf(target).Elem()
	field := v.FieldByName(fieldName)
	if !field.IsValid() || !field.CanSet() || field.Kind() != reflect.String {
		return
	}
	field.SetString(value)
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
