package backend

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/HeaInSeo/JUMI/pkg/metrics"
	"github.com/HeaInSeo/JUMI/pkg/provenance"
	"github.com/HeaInSeo/JUMI/pkg/spec"
	spimp "github.com/HeaInSeo/spawner/cmd/imp"
	spapi "github.com/HeaInSeo/spawner/pkg/api"
	spdriver "github.com/HeaInSeo/spawner/pkg/driver"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/remotecommand"
)

const outputManifestModeMetadataKey = "jumi.outputManifestMode"

// outputManifestModeWrappedShell is an obsolete compatibility mode kept only so
// older smoke/dev fixtures continue to run during the nan transition. It is a
// final removal target once JUMI switches fully to nan runtime execution.
const outputManifestModeWrappedShell = "wrapped-shell"

// outputManifestModeRuntimeHelper is an obsolete compatibility mode that still
// wraps the node command with the legacy helper path. New runtime integration
// should move to `nan run`. This mode is a final removal target.
const outputManifestModeRuntimeHelper = "runtime-helper"

// ArtifactHelperPath is the intended runtime binary path after node runtime
// images migrate to nan.
const ArtifactHelperPath = "/usr/local/bin/nan"

// ArtifactHelperPathEnv optionally overrides the helper path for runtime image
// migrations while keeping the legacy path as the default.
const ArtifactHelperPathEnv = "JUMI_ARTIFACT_HELPER_PATH"

type SpawnerK8sAdapter struct {
	driver     spdriver.Driver
	observer   *spimp.K8sObserver
	bounded    *spimp.BoundedDriver
	ns         string
	clientset  kubernetes.Interface
	restConfig *rest.Config
	metrics    *metrics.Metrics
}

func (a *SpawnerK8sAdapter) SetMetrics(m *metrics.Metrics) {
	a.metrics = m
}

type spawnerHandle struct {
	inner     spdriver.Handle
	jobName   string
	queueName string
}

type preparedSpawnerNode struct {
	inner              spdriver.Prepared
	runSpec            spapi.RunSpec
	workingDir         string
	serviceAccountName string
}

type directK8sHandle struct {
	jobName   string
	ns        string
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
	if a.metrics != nil {
		a.metrics.IncK8sNodePrepare()
	}
	runSpec := toSpawnerRunSpec(run, node)
	prepared, err := a.driver.Prepare(ctx, runSpec)
	if err != nil {
		if a.metrics != nil {
			a.metrics.IncK8sNodePrepareErrors()
		}
		return nil, err
	}
	return preparedSpawnerNode{
		inner:              prepared,
		runSpec:            runSpec,
		workingDir:         node.WorkingDir,
		serviceAccountName: node.ServiceAccountName,
	}, nil
}

func (a *SpawnerK8sAdapter) StartNode(ctx context.Context, prepared PreparedNode) (Handle, error) {
	if a.metrics != nil {
		a.metrics.IncK8sNodeStart()
	}
	if wrapped, ok := prepared.(preparedSpawnerNode); ok && shouldUseDirectK8sStart(wrapped) {
		handle, err := a.startDirectK8sNode(ctx, wrapped)
		if err != nil {
			if a.metrics != nil {
				a.metrics.IncK8sNodeStartErrors()
			}
			return nil, err
		}
		return handle, nil
	}
	if wrapped, ok := prepared.(preparedSpawnerNode); ok {
		prepared = wrapped.inner
	}
	spPrepared, ok := prepared.(spdriver.Prepared)
	if !ok {
		if a.metrics != nil {
			a.metrics.IncK8sNodeStartErrors()
		}
		return nil, fmt.Errorf("unexpected prepared type %T", prepared)
	}
	handle, err := a.driver.Start(ctx, spPrepared)
	if err != nil {
		if a.metrics != nil {
			a.metrics.IncK8sNodeStartErrors()
		}
		return nil, err
	}
	jobName, _ := extractHandleJobName(handle)
	queueName := extractPreparedQueueName(prepared)
	return spawnerHandle{inner: handle, jobName: jobName, queueName: queueName}, nil
}

func (a *SpawnerK8sAdapter) ObserveNode(ctx context.Context, handle Handle) (*OptionalKueueInfo, error) {
	var h spawnerHandle
	switch typed := handle.(type) {
	case spawnerHandle:
		h = typed
	case directK8sHandle:
		h = spawnerHandle{jobName: typed.jobName, queueName: typed.queueName}
	default:
		return nil, nil
	}
	if h.jobName == "" || a.observer == nil {
		return nil, nil
	}
	info := &OptionalKueueInfo{Observed: false, QueueName: h.queueName}
	if h.queueName == "" {
		if err := a.fillPodObservation(ctx, h.jobName, info); err != nil {
			return nil, nil
		}
		info.Observed = true
		return info, nil
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		obs, err := a.observer.ObserveWorkload(ctx, h.jobName)
		if err == nil {
			info.Observed = true
			info.WorkloadName = obs.WorkloadName
			info.PendingReason = obs.PendingReason
			info.Admitted = obs.Admitted
			if obs.Admitted {
				_ = a.fillPodObservation(ctx, h.jobName, info)
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

func (a *SpawnerK8sAdapter) fillPodObservation(ctx context.Context, jobName string, info *OptionalKueueInfo) error {
	if info == nil {
		return fmt.Errorf("nil observation info")
	}
	podObs, err := a.observer.ObservePod(ctx, jobName)
	if err != nil {
		return err
	}
	info.PodName = podObs.PodName
	info.Scheduled = podObs.Scheduled
	info.UnschedulableReason = podObs.UnschedulableReason
	if a.clientset == nil || podObs.PodName == "" {
		return nil
	}
	pod, err := a.clientset.CoreV1().Pods(a.ns).Get(ctx, podObs.PodName, metav1.GetOptions{})
	if err != nil {
		return err
	}
	info.PodNodeName = pod.Spec.NodeName
	return nil
}

func (a *SpawnerK8sAdapter) WaitNode(ctx context.Context, handle Handle) (ExecutionResult, error) {
	if h, ok := handle.(directK8sHandle); ok {
		return a.waitDirectK8sNode(ctx, h)
	}
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
		if a.metrics != nil {
			a.metrics.IncK8sNodeSucceeded()
		}
	case spapi.StateCancelled:
		result.TerminalStopCause = "canceled"
		result.TerminalFailureReason = "cancellation_propagated"
		if a.metrics != nil {
			a.metrics.IncK8sNodeCanceled()
		}
		return result, fmt.Errorf("node canceled")
	case spapi.StateFailed:
		result.TerminalStopCause = "failed"
		result.TerminalFailureReason = "backend_wait_error"
		if a.metrics != nil {
			a.metrics.IncK8sNodeFailed()
		}
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
	if a.metrics != nil {
		a.metrics.IncK8sNodeCancel()
	}
	if h, ok := handle.(directK8sHandle); ok {
		propagation := metav1.DeletePropagationBackground
		return a.clientset.BatchV1().Jobs(h.ns).Delete(ctx, h.jobName, metav1.DeleteOptions{
			PropagationPolicy: &propagation,
		})
	}
	h, ok := handle.(spawnerHandle)
	if !ok {
		return fmt.Errorf("unexpected handle type %T", handle)
	}
	return a.driver.Cancel(ctx, h.inner)
}

func (a *SpawnerK8sAdapter) CollectOutputMetadata(ctx context.Context, handle Handle, node spec.Node) (map[string]OutputMetadata, error) {
	namespace, jobName, ok := a.outputMetadataJobRef(handle)
	if !ok {
		return nil, ErrOutputMetadataUnavailable
	}
	if jobName == "" || a.clientset == nil || a.restConfig == nil {
		return nil, ErrOutputMetadataUnavailable
	}
	pod, err := a.findJobPod(ctx, namespace, jobName)
	if err != nil {
		return nil, ErrOutputMetadataUnavailable
	}
	raw, err := readArtifactsManifestFromTerminationMessage(pod)
	if err != nil {
		return nil, fmt.Errorf("read artifacts manifest from pod termination message %s: %w", pod.Name, err)
	}
	if len(raw) == 0 {
		raw, err = a.readArtifactsManifest(ctx, namespace, pod.Name, manifestPathForNode(node))
		if err != nil {
			return nil, ErrOutputMetadataUnavailable
		}
	}
	manifest, err := provenance.ParseArtifactManifest(raw)
	if err != nil {
		return nil, fmt.Errorf("parse artifacts manifest for pod %s: %w", pod.Name, err)
	}
	if err := validateObservedManifest(manifest, node); err != nil {
		return nil, fmt.Errorf("validate artifacts manifest for pod %s: %w", pod.Name, err)
	}
	out := make(map[string]OutputMetadata, len(manifest.Artifacts))
	for _, outputName := range node.Outputs {
		record, ok := manifest.ByOutputName(outputName)
		if !ok {
			continue
		}
		out[outputName] = OutputMetadata{
			OutputName:        outputName,
			URI:               record.URI,
			LogicalURI:        record.LogicalURI,
			Digest:            record.Digest,
			SizeBytes:         record.SizeBytes,
			ProducerAttemptID: outputProducerAttemptID(record, manifest),
			Locations:         append([]provenance.ArtifactLocation(nil), record.Locations...),
			NodeName:          pod.Spec.NodeName,
			PodName:           pod.Name,
		}
	}
	if len(out) == 0 {
		return nil, ErrOutputMetadataUnavailable
	}
	return out, nil
}

func outputProducerAttemptID(record provenance.ArtifactRecord, manifest provenance.ArtifactManifest) string {
	if strings.TrimSpace(record.ProducerAttemptID) != "" {
		return record.ProducerAttemptID
	}
	return manifest.AttemptID
}

func validateObservedManifest(manifest provenance.ArtifactManifest, node spec.Node) error {
	if node.Env == nil {
		return nil
	}
	if expected := strings.TrimSpace(node.Env["JUMI_RUN_ID"]); expected != "" {
		if manifest.RunID == "" {
			return fmt.Errorf("manifest runId is required")
		}
		if manifest.RunID != expected {
			return fmt.Errorf("manifest runId = %q, want %q", manifest.RunID, expected)
		}
	}
	if expected := strings.TrimSpace(node.Env["JUMI_NODE_ID"]); expected != "" {
		if manifest.NodeID == "" {
			return fmt.Errorf("manifest nodeId is required")
		}
		if manifest.NodeID != expected {
			return fmt.Errorf("manifest nodeId = %q, want %q", manifest.NodeID, expected)
		}
	}
	if expected := strings.TrimSpace(node.Env["JUMI_ATTEMPT_ID"]); expected != "" {
		if manifest.AttemptID == "" {
			return fmt.Errorf("manifest attemptId is required")
		}
		if manifest.AttemptID != expected {
			return fmt.Errorf("manifest attemptId = %q, want %q", manifest.AttemptID, expected)
		}
	}
	return nil
}

func (a *SpawnerK8sAdapter) outputMetadataJobRef(handle Handle) (string, string, bool) {
	switch h := handle.(type) {
	case spawnerHandle:
		return a.ns, h.jobName, h.jobName != ""
	case directK8sHandle:
		return h.ns, h.jobName, h.jobName != ""
	default:
		return "", "", false
	}
}

func (a *SpawnerK8sAdapter) findJobPod(ctx context.Context, namespace string, jobName string) (*corev1.Pod, error) {
	pods, err := a.clientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: fmt.Sprintf("job-name=%s", jobName),
	})
	if err != nil {
		return nil, err
	}
	if len(pods.Items) == 0 {
		return nil, fmt.Errorf("no pods found for job %s", jobName)
	}
	var best *corev1.Pod
	for i := range pods.Items {
		pod := &pods.Items[i]
		if !mainContainerSucceeded(pod) {
			continue
		}
		if best == nil || podSucceededLater(pod, best) {
			best = pod
		}
	}
	if best != nil {
		return best, nil
	}
	for i := range pods.Items {
		pod := &pods.Items[i]
		if mainContainerTerminated(pod) {
			if best == nil || podTerminatedLater(pod, best) {
				best = pod
			}
		}
	}
	if best != nil {
		return best, nil
	}
	return &pods.Items[0], nil
}

func mainContainerSucceeded(pod *corev1.Pod) bool {
	if pod == nil {
		return false
	}
	for _, status := range pod.Status.ContainerStatuses {
		if status.Name != "main" || status.State.Terminated == nil {
			continue
		}
		return status.State.Terminated.ExitCode == 0
	}
	return false
}

func mainContainerTerminated(pod *corev1.Pod) bool {
	if pod == nil {
		return false
	}
	for _, status := range pod.Status.ContainerStatuses {
		if status.Name == "main" && status.State.Terminated != nil {
			return true
		}
	}
	return false
}

func podSucceededLater(candidate *corev1.Pod, current *corev1.Pod) bool {
	return terminatedAt(candidate).After(terminatedAt(current))
}

func podTerminatedLater(candidate *corev1.Pod, current *corev1.Pod) bool {
	return terminatedAt(candidate).After(terminatedAt(current))
}

func terminatedAt(pod *corev1.Pod) time.Time {
	if pod == nil {
		return time.Time{}
	}
	for _, status := range pod.Status.ContainerStatuses {
		if status.Name != "main" || status.State.Terminated == nil {
			continue
		}
		return status.State.Terminated.FinishedAt.Time
	}
	return time.Time{}
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

func manifestPathForNode(node spec.Node) string {
	if node.Env != nil {
		if path := strings.TrimSpace(node.Env["JUMI_OUTPUT_MANIFEST_PATH"]); path != "" {
			return path
		}
	}
	return provenance.DefaultArtifactsManifestPath
}

func (a *SpawnerK8sAdapter) readArtifactsManifest(ctx context.Context, namespace string, podName string, manifestPath string) ([]byte, error) {
	if strings.TrimSpace(namespace) == "" {
		namespace = a.ns
	}
	if strings.TrimSpace(manifestPath) == "" {
		manifestPath = provenance.DefaultArtifactsManifestPath
	}
	req := a.clientset.CoreV1().RESTClient().Post().
		Namespace(namespace).
		Resource("pods").
		Name(podName).
		SubResource("exec")
	req.VersionedParams(&corev1.PodExecOptions{
		Container: "main",
		Command:   []string{"cat", manifestPath},
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
	setEnvDefault(env, "JUMI_OUTPUT_MANIFEST_ENABLED", "true")
	setEnvDefault(env, "JUMI_OUTPUT_ROOT", "/out")
	setEnvDefault(env, "JUMI_OUTPUT_MANIFEST_PATH", provenance.DefaultArtifactsManifestPath)
	setEnvDefault(env, "JUMI_OUTPUT_NAMES", strings.Join(outputs, ","))
	setEnvDefault(env, "JUMI_RUN_ID", run.RunID)
	setEnvDefault(env, "JUMI_NODE_ID", node.NodeID)
	if sampleRunID := run.Spec.Run.SampleRunID; sampleRunID != "" {
		setEnvDefault(env, "JUMI_SAMPLE_RUN_ID", sampleRunID)
	}
}

func setEnvDefault(env map[string]string, key string, value string) {
	if _, exists := env[key]; exists {
		return
	}
	env[key] = value
}

func wrapCommandForManifestExport(command []string, node spec.Node) []string {
	mode := manifestExportMode(node)
	if mode == "" || len(command) == 0 {
		return command
	}
	if mode == outputManifestModeRuntimeHelper {
		// Compatibility note:
		// The runtime-side artifact helper belongs to the DAG node runtime image,
		// not to the JUMI service image. Use the canonical nan path by default and
		// keep the legacy helper name only as a transitional runtime image alias.
		//
		// TODO(runtime-contract): remove this obsolete compatibility path after
		// the node runtime images no longer carry the legacy helper alias.
		wrapped := []string{artifactHelperCommandPath(), "run", "--"}
		wrapped = append(wrapped, command...)
		return wrapped
	}
	// wrapped-shell is obsolete compatibility only. Keep it available for older
	// fixtures until nan command injection fully replaces shell wrapping.
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

func artifactHelperCommandPath() string {
	if configured := strings.TrimSpace(os.Getenv(ArtifactHelperPathEnv)); configured != "" {
		return configured
	}
	return ArtifactHelperPath
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
	if node.Placement == nil {
		return nil
	}
	out := &spapi.Placement{}
	if len(node.Placement.NodeSelector) > 0 {
		out.NodeSelector = make(map[string]string, len(node.Placement.NodeSelector))
		for k, v := range node.Placement.NodeSelector {
			out.NodeSelector[k] = v
		}
	}
	out.RequiredNodeName = node.Placement.RequiredNodeName
	if len(node.Placement.PreferredNodes) > 0 {
		out.PreferredNodes = make([]spapi.WeightedNodePreference, 0, len(node.Placement.PreferredNodes))
		for _, pref := range node.Placement.PreferredNodes {
			out.PreferredNodes = append(out.PreferredNodes, spapi.WeightedNodePreference{
				NodeName: pref.NodeName,
				Weight:   pref.Weight,
			})
		}
	}
	if len(out.NodeSelector) == 0 && out.RequiredNodeName == "" && len(out.PreferredNodes) == 0 {
		return nil
	}
	return out
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

func shouldUseDirectK8sStart(prepared preparedSpawnerNode) bool {
	return prepared.serviceAccountName != "" || prepared.workingDir != "" || hasDirectHostPathMount(prepared.runSpec.Mounts)
}

func (a *SpawnerK8sAdapter) startDirectK8sNode(ctx context.Context, prepared preparedSpawnerNode) (Handle, error) {
	if a.clientset == nil {
		return nil, fmt.Errorf("direct k8s start requires clientset")
	}
	job := buildDirectK8sJob(prepared.runSpec, a.ns, prepared.workingDir, prepared.serviceAccountName)
	created, err := a.clientset.BatchV1().Jobs(a.ns).Create(ctx, job, metav1.CreateOptions{})
	if err != nil {
		return nil, fmt.Errorf("create direct job %s: %w", job.Name, err)
	}
	return directK8sHandle{
		jobName:   created.Name,
		ns:        created.Namespace,
		queueName: extractQueueName(created.Labels),
	}, nil
}

func buildDirectK8sJob(runSpec spapi.RunSpec, ns, workingDir, serviceAccountName string) *batchv1.Job {
	container := corev1.Container{
		Name:            "main",
		Image:           runSpec.ImageRef,
		Command:         runSpec.Command,
		WorkingDir:      workingDir,
		Env:             buildDirectEnvVars(runSpec.Env, runSpec.EnvFieldRefs),
		Resources:       buildDirectResources(runSpec.Resources),
		ImagePullPolicy: corev1.PullIfNotPresent,
	}

	volumes, volumeMounts := buildDirectVolumes(runSpec.Mounts)
	container.VolumeMounts = volumeMounts

	labels := make(map[string]string, len(runSpec.Labels)+1)
	labels["pipeline-lite/run-id"] = runSpec.RunID
	for k, v := range runSpec.Labels {
		labels[k] = v
	}

	annotations := make(map[string]string, len(runSpec.Annotations)+1)
	for k, v := range runSpec.Annotations {
		annotations[k] = v
	}
	if runSpec.CorrelationID != "" {
		annotations["spawner.correlation-id"] = runSpec.CorrelationID
	}

	_, useKueue := labels["kueue.x-k8s.io/queue-name"]
	suspend := useKueue
	backoffLimit := int32(0)
	var ttlSecondsAfterFinished *int32
	if runSpec.Cleanup.TTLSecondsAfterFinished > 0 {
		ttl := runSpec.Cleanup.TTLSecondsAfterFinished
		ttlSecondsAfterFinished = &ttl
	}

	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:        directSanitizeName(runSpec.RunID),
			Namespace:   ns,
			Labels:      labels,
			Annotations: annotations,
		},
		Spec: batchv1.JobSpec{
			Suspend:                 &suspend,
			BackoffLimit:            &backoffLimit,
			TTLSecondsAfterFinished: ttlSecondsAfterFinished,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      labels,
					Annotations: annotations,
				},
				Spec: corev1.PodSpec{
					ServiceAccountName: serviceAccountName,
					RestartPolicy:      corev1.RestartPolicyNever,
					Containers:         []corev1.Container{container},
					Volumes:            volumes,
					NodeSelector:       buildDirectNodeSelector(runSpec.Placement),
					Affinity:           buildDirectAffinity(runSpec.Placement),
				},
			},
		},
	}
}

func buildDirectEnvVars(env map[string]string, fieldRefs map[string]string) []corev1.EnvVar {
	vars := make([]corev1.EnvVar, 0, len(env)+len(fieldRefs))
	for k, v := range env {
		vars = append(vars, corev1.EnvVar{Name: k, Value: v})
	}
	for k, path := range fieldRefs {
		vars = append(vars, corev1.EnvVar{
			Name: k,
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{FieldPath: path},
			},
		})
	}
	return vars
}

func buildDirectResources(r spapi.Resources) corev1.ResourceRequirements {
	req := corev1.ResourceList{}
	lim := corev1.ResourceList{}
	if r.CPU != "" {
		q := resource.MustParse(r.CPU)
		req[corev1.ResourceCPU] = q
		lim[corev1.ResourceCPU] = q
	}
	if r.Memory != "" {
		q := resource.MustParse(r.Memory)
		req[corev1.ResourceMemory] = q
		lim[corev1.ResourceMemory] = q
	}
	return corev1.ResourceRequirements{Requests: req, Limits: lim}
}

func buildDirectVolumes(mounts []spapi.Mount) ([]corev1.Volume, []corev1.VolumeMount) {
	volumes := make([]corev1.Volume, 0, len(mounts))
	volumeMounts := make([]corev1.VolumeMount, 0, len(mounts))
	for i, m := range mounts {
		volName := fmt.Sprintf("vol-%d", i)
		volume := corev1.Volume{Name: volName}
		if hostPath, ok := directHostPathSource(m.Source); ok {
			volume.VolumeSource = corev1.VolumeSource{
				HostPath: &corev1.HostPathVolumeSource{
					Path: hostPath,
				},
			}
		} else {
			volume.VolumeSource = corev1.VolumeSource{
				PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
					ClaimName: m.Source,
					ReadOnly:  m.ReadOnly,
				},
			}
		}
		volumes = append(volumes, volume)
		volumeMounts = append(volumeMounts, corev1.VolumeMount{
			Name:      volName,
			MountPath: m.Target,
			ReadOnly:  m.ReadOnly,
		})
	}
	return volumes, volumeMounts
}

func hasDirectHostPathMount(mounts []spapi.Mount) bool {
	for _, m := range mounts {
		if _, ok := directHostPathSource(m.Source); ok {
			return true
		}
	}
	return false
}

func directHostPathSource(source string) (string, bool) {
	const prefix = "hostpath:"
	if !strings.HasPrefix(source, prefix) {
		return "", false
	}
	path := strings.TrimSpace(strings.TrimPrefix(source, prefix))
	if path == "" {
		return "", false
	}
	return path, true
}

func buildDirectNodeSelector(p *spapi.Placement) map[string]string {
	if p == nil {
		return nil
	}
	size := len(p.NodeSelector)
	if p.RequiredNodeName != "" {
		size++
	}
	if size == 0 {
		return nil
	}
	out := make(map[string]string, size)
	for k, v := range p.NodeSelector {
		out[k] = v
	}
	if p.RequiredNodeName != "" {
		out["kubernetes.io/hostname"] = p.RequiredNodeName
	}
	return out
}

func buildDirectAffinity(p *spapi.Placement) *corev1.Affinity {
	if p == nil || len(p.PreferredNodes) == 0 {
		return nil
	}
	terms := make([]corev1.PreferredSchedulingTerm, 0, len(p.PreferredNodes))
	for _, pref := range p.PreferredNodes {
		terms = append(terms, corev1.PreferredSchedulingTerm{
			Weight: pref.Weight,
			Preference: corev1.NodeSelectorTerm{
				MatchExpressions: []corev1.NodeSelectorRequirement{{
					Key:      "kubernetes.io/hostname",
					Operator: corev1.NodeSelectorOpIn,
					Values:   []string{pref.NodeName},
				}},
			},
		})
	}
	return &corev1.Affinity{
		NodeAffinity: &corev1.NodeAffinity{
			PreferredDuringSchedulingIgnoredDuringExecution: terms,
		},
	}
}

func directSanitizeName(id string) string {
	s := strings.ToLower(id)
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			b.WriteRune(r)
		} else {
			b.WriteRune('-')
		}
	}
	name := strings.Trim(b.String(), "-")
	if len(name) > 63 {
		name = name[:63]
	}
	return name
}

func extractQueueName(labels map[string]string) string {
	if labels == nil {
		return ""
	}
	return labels["kueue.x-k8s.io/queue-name"]
}

func (a *SpawnerK8sAdapter) waitDirectK8sNode(ctx context.Context, h directK8sHandle) (ExecutionResult, error) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ExecutionResult{TerminalStopCause: "canceled", TerminalFailureReason: "cancellation_requested"}, ctx.Err()
		case <-ticker.C:
			job, err := a.clientset.BatchV1().Jobs(h.ns).Get(ctx, h.jobName, metav1.GetOptions{})
			if err != nil {
				return ExecutionResult{TerminalStopCause: "failed", TerminalFailureReason: "backend_wait_error"}, fmt.Errorf("get direct job %s: %w", h.jobName, err)
			}
			if directJobSucceeded(job) {
				if a.metrics != nil {
					a.metrics.IncK8sNodeSucceeded()
				}
				return ExecutionResult{Succeeded: true, TerminalStopCause: "finished"}, nil
			}
			if directJobFailed(job) {
				if a.metrics != nil {
					a.metrics.IncK8sNodeFailed()
				}
				msg := directJobFailureMessage(job)
				result := ExecutionResult{TerminalStopCause: "failed", TerminalFailureReason: "backend_wait_error"}
				if strings.TrimSpace(msg) != "" {
					return result, fmt.Errorf("node failed: %s", msg)
				}
				return result, fmt.Errorf("node failed")
			}
		}
	}
}

func directJobSucceeded(job *batchv1.Job) bool {
	for _, c := range job.Status.Conditions {
		if c.Type == batchv1.JobComplete && c.Status == corev1.ConditionTrue {
			return true
		}
	}
	return false
}

func directJobFailed(job *batchv1.Job) bool {
	for _, c := range job.Status.Conditions {
		if c.Type == batchv1.JobFailed && c.Status == corev1.ConditionTrue {
			return true
		}
	}
	return false
}

func directJobFailureMessage(job *batchv1.Job) string {
	for _, c := range job.Status.Conditions {
		if c.Type == batchv1.JobFailed && c.Status == corev1.ConditionTrue {
			if strings.TrimSpace(c.Message) != "" {
				return c.Message
			}
			return c.Reason
		}
	}
	return ""
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
