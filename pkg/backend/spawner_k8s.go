package backend

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/HeaInSeo/JUMI/pkg/metrics"
	"github.com/HeaInSeo/JUMI/pkg/provenance"
	"github.com/HeaInSeo/JUMI/pkg/spec"
	spimp "github.com/HeaInSeo/spawner/cmd/imp"
	spruntime "github.com/HeaInSeo/spawner/pkg/runtime"
	corev1 "k8s.io/api/core/v1"
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

const (
	nodeContractSchemaVersion = "nan.nodeContract.v1"
	defaultNodeContractPath   = "/jumi/node-contract.json"
)

type SpawnerK8sAdapter struct {
	observer   *spimp.K8sObserver
	ns         string
	clientset  kubernetes.Interface
	restConfig *rest.Config
	metrics    *metrics.Metrics

	// rt, when non-nil, enables the spawner Runtime path introduced in S5.
	// Methods branch to the Runtime path when rt != nil.
	rt spruntime.Runtime
}

// preparedRuntimeNode is the PreparedNode returned by PrepareNode when using the Runtime path.
type preparedRuntimeNode struct {
	req       spruntime.AttemptRequest
	queueName string // extracted from Kueue hints; used by ObserveNode
}

// runtimeHandle is the Handle returned by StartNode when using the Runtime path.
type runtimeHandle struct {
	handle    spruntime.AttemptHandle
	queueName string
}

// NewSpawnerK8sAdapterWithRuntime creates an adapter that routes job execution
// through a spawner Runtime. The caller constructs the Runtime with a K8sJobClient
// (see pkg/spawner.NewK8sJobClient) before calling this constructor.
//
// observer may be nil; Kueue observation is disabled when nil.
func NewSpawnerK8sAdapterWithRuntime(
	rt spruntime.Runtime,
	namespace string,
	clientset kubernetes.Interface,
	restConfig *rest.Config,
	observer *spimp.K8sObserver,
) *SpawnerK8sAdapter {
	return &SpawnerK8sAdapter{
		rt:         rt,
		ns:         namespace,
		clientset:  clientset,
		restConfig: restConfig,
		observer:   observer,
	}
}

func (a *SpawnerK8sAdapter) SetMetrics(m *metrics.Metrics) {
	a.metrics = m
}

func (a *SpawnerK8sAdapter) AdapterStatus() AdapterStatus {
	status := AdapterStatus{Ready: a.rt != nil}
	return status
}

func (a *SpawnerK8sAdapter) PrepareNode(ctx context.Context, run spec.RunRecord, node spec.Node) (PreparedNode, error) {
	if a.metrics != nil {
		a.metrics.IncK8sNodePrepare()
	}
	return preparedRuntimeNode{
		req:       toAttemptRequest(run, node),
		queueName: kueueQueueName(node),
	}, nil
}

func (a *SpawnerK8sAdapter) StartNode(ctx context.Context, prepared PreparedNode) (Handle, error) {
	if a.metrics != nil {
		a.metrics.IncK8sNodeStart()
	}
	n, ok := prepared.(preparedRuntimeNode)
	if !ok {
		if a.metrics != nil {
			a.metrics.IncK8sNodeStartErrors()
		}
		return nil, fmt.Errorf("unexpected prepared type %T", prepared)
	}
	handle, err := a.rt.SubmitAttempt(ctx, n.req)
	if err != nil {
		if a.metrics != nil {
			a.metrics.IncK8sNodeStartErrors()
		}
		return nil, err
	}
	return runtimeHandle{handle: handle, queueName: n.queueName}, nil
}

func (a *SpawnerK8sAdapter) ObserveNode(ctx context.Context, handle Handle) (*OptionalKueueInfo, error) {
	rh, ok := handle.(runtimeHandle)
	if !ok {
		return nil, nil
	}
	jobName := rh.handle.BackendRef.Name
	if jobName == "" || a.observer == nil {
		return nil, nil
	}
	queueName := rh.queueName
	info := &OptionalKueueInfo{Observed: false, QueueName: queueName}
	if queueName == "" {
		if err := a.fillPodObservation(ctx, jobName, info); err != nil {
			return nil, nil
		}
		info.Observed = true
		return info, nil
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		obs, err := a.observer.ObserveWorkload(ctx, jobName)
		if err == nil {
			info.Observed = true
			info.WorkloadName = obs.WorkloadName
			info.PendingReason = obs.PendingReason
			info.Admitted = obs.Admitted
			if obs.Admitted {
				_ = a.fillPodObservation(ctx, jobName, info)
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
	h, ok := handle.(runtimeHandle)
	if !ok {
		return ExecutionResult{TerminalStopCause: "failed", TerminalFailureReason: "backend_wait_error"}, fmt.Errorf("unexpected handle type %T", handle)
	}
	return a.waitRuntimeHandle(ctx, h)
}

// waitRuntimeHandle blocks until the Runtime emits a terminal event for h.handle.
func (a *SpawnerK8sAdapter) waitRuntimeHandle(ctx context.Context, h runtimeHandle) (ExecutionResult, error) {
	ch, err := a.rt.WatchAttempt(ctx, h.handle)
	if err != nil {
		return ExecutionResult{TerminalStopCause: "failed", TerminalFailureReason: "backend_wait_error"}, err
	}
	var last spruntime.AttemptEvent
waitLoop:
	for {
		select {
		case ev, ok := <-ch:
			if !ok {
				break waitLoop
			}
			last = ev
		case <-ctx.Done():
			return ExecutionResult{TerminalStopCause: "canceled", TerminalFailureReason: "cancellation_requested"}, ctx.Err()
		}
	}
	switch last.State {
	case spruntime.AttemptStateSucceeded:
		if a.metrics != nil {
			a.metrics.IncK8sNodeSucceeded()
		}
		return ExecutionResult{Succeeded: true, TerminalStopCause: "finished"}, nil
	case spruntime.AttemptStateCancelled:
		if a.metrics != nil {
			a.metrics.IncK8sNodeCanceled()
		}
		return ExecutionResult{TerminalStopCause: "canceled", TerminalFailureReason: "cancellation_propagated"}, fmt.Errorf("node canceled")
	default:
		if a.metrics != nil {
			a.metrics.IncK8sNodeFailed()
		}
		msg := strings.TrimSpace(last.Message)
		if msg == "" {
			msg = "node failed"
		}
		return ExecutionResult{
			TerminalStopCause:     "failed",
			TerminalFailureReason: runtimeReasonToFailure(last.Reason),
		}, fmt.Errorf("node failed: %s", msg)
	}
}

func (a *SpawnerK8sAdapter) CancelNode(ctx context.Context, handle Handle) error {
	if a.metrics != nil {
		a.metrics.IncK8sNodeCancel()
	}
	h, ok := handle.(runtimeHandle)
	if !ok {
		return fmt.Errorf("unexpected handle type %T", handle)
	}
	return a.rt.CancelAttempt(ctx, h.handle)
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
	if manifest.SchemaVersion != "" {
		switch manifest.SchemaVersion {
		case provenance.ArtifactManifestSchemaVersion, "nan.artifactManifest.v1":
		default:
			return fmt.Errorf("unsupported manifest schemaVersion %q", manifest.SchemaVersion)
		}
	}
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
	h, ok := handle.(runtimeHandle)
	if !ok {
		return "", "", false
	}
	return h.handle.BackendRef.Namespace, h.handle.BackendRef.Name, h.handle.BackendRef.Name != ""
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

// BuildK8sRestConfig constructs a *rest.Config from an optional kubeconfig path.
// If kubeconfigPath is empty, it falls back to in-cluster config, then the default
// kubeconfig loading rules.
func BuildK8sRestConfig(kubeconfigPath string) (*rest.Config, error) {
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

var contractInputEnvSuffixes = []string{
	"_URI",
	"_EXPECTED_DIGEST",
	"_EXPECTED_SIZE_BYTES",
	"_MATERIALIZATION_MODE",
	"_NODE_LOCAL_PATH",
	"_LOCAL_PATH",
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
	if contractJSON, err := buildNodeContractJSON(run, node, outputs); err == nil {
		setEnvDefault(env, "JUMI_NODE_CONTRACT_PATH", defaultNodeContractPath)
		setEnvDefault(env, "JUMI_NODE_CONTRACT_JSON", contractJSON)
		if manifestExportMode(node) == outputManifestModeRuntimeHelper {
			stripContractInputEnv(env)
		}
	}
}

func stripContractInputEnv(env map[string]string) {
	for key := range env {
		if !strings.HasPrefix(key, "JUMI_INPUT_") {
			continue
		}
		for _, suffix := range contractInputEnvSuffixes {
			if strings.HasSuffix(key, suffix) {
				delete(env, key)
				break
			}
		}
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
		script := fmt.Sprintf(`
contract_path="${JUMI_NODE_CONTRACT_PATH:-%s}"
contract_dir="${contract_path%%/*}"
if [ "$contract_dir" = "$contract_path" ]; then
  contract_dir="."
fi
mkdir -p "${contract_path%%/*}"
printf '%%s' "${JUMI_NODE_CONTRACT_JSON:?missing JUMI_NODE_CONTRACT_JSON}" > "$contract_path"
exec "%s" run --contract "$contract_path" -- "$@"
`, defaultNodeContractPath, artifactHelperCommandPath())
		wrapped := []string{"/bin/sh", "-ceu", script, "jumi-node-contract"}
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

type nodeContractFile struct {
	SchemaVersion string               `json:"schemaVersion"`
	RunID         string               `json:"runId"`
	SampleRunID   string               `json:"sampleRunId,omitempty"`
	NodeID        string               `json:"nodeId"`
	AttemptID     string               `json:"attemptId,omitempty"`
	Paths         nodeContractPaths    `json:"paths"`
	Inputs        []nodeContractInput  `json:"inputs,omitempty"`
	Outputs       []nodeContractOutput `json:"outputs,omitempty"`
	Runtime       nodeContractRuntime  `json:"runtime,omitempty"`
}

type nodeContractPaths struct {
	InputRoot    string `json:"inputRoot,omitempty"`
	WorkRoot     string `json:"workRoot,omitempty"`
	OutputRoot   string `json:"outputRoot"`
	ManifestPath string `json:"manifestPath"`
}

type nodeContractOutput struct {
	Name     string `json:"name"`
	Path     string `json:"path"`
	Required bool   `json:"required,omitempty"`
	Type     string `json:"type,omitempty"`
}

type nodeContractInput struct {
	Name                string `json:"name"`
	URI                 string `json:"uri,omitempty"`
	ExpectedDigest      string `json:"expectedDigest,omitempty"`
	ExpectedSizeBytes   int64  `json:"expectedSizeBytes,omitempty"`
	MaterializationMode string `json:"materializationMode,omitempty"`
	NodeLocalPath       string `json:"nodeLocalPath,omitempty"`
	LocalPath           string `json:"localPath,omitempty"`
}

type partialNodeContractInput struct {
	uri                 string
	expectedDigest      string
	expectedSizeBytes   string
	materializationMode string
	nodeLocalPath       string
	localPath           string
}

type nodeContractRuntime struct {
	InspectOnSuccessOnly        bool `json:"inspectOnSuccessOnly,omitempty"`
	FailOnMissingRequiredOutput bool `json:"failOnMissingRequiredOutput,omitempty"`
	AllowDirectoryOutput        bool `json:"allowDirectoryOutput,omitempty"`
}

func buildNodeContractJSON(run spec.RunRecord, node spec.Node, outputs []string) (string, error) {
	contract := nodeContractFile{
		SchemaVersion: nodeContractSchemaVersion,
		RunID:         run.RunID,
		SampleRunID:   run.Spec.Run.SampleRunID,
		NodeID:        node.NodeID,
		AttemptID:     strings.TrimSpace(node.Env["JUMI_ATTEMPT_ID"]),
		Paths: nodeContractPaths{
			InputRoot:    filepath.Join("/work", "inputs"),
			WorkRoot:     "/work",
			OutputRoot:   strings.TrimSpace(contractFirstNonEmpty(node.Env["JUMI_OUTPUT_ROOT"], "/out")),
			ManifestPath: strings.TrimSpace(contractFirstNonEmpty(node.Env["JUMI_OUTPUT_MANIFEST_PATH"], provenance.DefaultArtifactsManifestPath)),
		},
		Runtime: nodeContractRuntime{
			InspectOnSuccessOnly:        true,
			FailOnMissingRequiredOutput: true,
			AllowDirectoryOutput:        false,
		},
	}
	if len(outputs) != 0 {
		contract.Outputs = make([]nodeContractOutput, 0, len(outputs))
		for _, output := range outputs {
			if strings.TrimSpace(output) == "" {
				continue
			}
			contract.Outputs = append(contract.Outputs, nodeContractOutput{
				Name:     output,
				Path:     output,
				Required: true,
				Type:     "file",
			})
		}
	}
	contract.Inputs = buildNodeContractInputs(node.Env)
	raw, err := json.Marshal(contract)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func buildNodeContractInputs(env map[string]string) []nodeContractInput {
	byBase := map[string]*partialNodeContractInput{}
	for key, value := range env {
		if !strings.HasPrefix(key, "JUMI_INPUT_") {
			continue
		}
		switch {
		case strings.HasSuffix(key, "_URI"):
			base := strings.TrimSuffix(strings.TrimPrefix(key, "JUMI_INPUT_"), "_URI")
			p := ensureNodeContractInput(byBase, base)
			p.uri = value
		case strings.HasSuffix(key, "_EXPECTED_DIGEST"):
			base := strings.TrimSuffix(strings.TrimPrefix(key, "JUMI_INPUT_"), "_EXPECTED_DIGEST")
			p := ensureNodeContractInput(byBase, base)
			p.expectedDigest = value
		case strings.HasSuffix(key, "_EXPECTED_SIZE_BYTES"):
			base := strings.TrimSuffix(strings.TrimPrefix(key, "JUMI_INPUT_"), "_EXPECTED_SIZE_BYTES")
			p := ensureNodeContractInput(byBase, base)
			p.expectedSizeBytes = value
		case strings.HasSuffix(key, "_MATERIALIZATION_MODE"):
			base := strings.TrimSuffix(strings.TrimPrefix(key, "JUMI_INPUT_"), "_MATERIALIZATION_MODE")
			p := ensureNodeContractInput(byBase, base)
			p.materializationMode = value
		case strings.HasSuffix(key, "_NODE_LOCAL_PATH"):
			base := strings.TrimSuffix(strings.TrimPrefix(key, "JUMI_INPUT_"), "_NODE_LOCAL_PATH")
			p := ensureNodeContractInput(byBase, base)
			p.nodeLocalPath = value
		case strings.HasSuffix(key, "_LOCAL_PATH"):
			base := strings.TrimSuffix(strings.TrimPrefix(key, "JUMI_INPUT_"), "_LOCAL_PATH")
			p := ensureNodeContractInput(byBase, base)
			p.localPath = value
		}
	}
	if len(byBase) == 0 {
		return nil
	}
	names := make([]string, 0, len(byBase))
	for base := range byBase {
		names = append(names, base)
	}
	sort.Strings(names)
	inputs := make([]nodeContractInput, 0, len(names))
	for _, base := range names {
		p := byBase[base]
		mode := strings.TrimSpace(p.materializationMode)
		if mode == "" || mode == "none" {
			continue
		}
		input := nodeContractInput{
			Name:                strings.ToLower(base),
			URI:                 strings.TrimSpace(p.uri),
			ExpectedDigest:      strings.TrimSpace(p.expectedDigest),
			MaterializationMode: mode,
			NodeLocalPath:       strings.TrimSpace(p.nodeLocalPath),
			LocalPath:           strings.TrimSpace(p.localPath),
		}
		if sizeText := strings.TrimSpace(p.expectedSizeBytes); sizeText != "" {
			if size, err := strconv.ParseInt(sizeText, 10, 64); err == nil && size >= 0 {
				input.ExpectedSizeBytes = size
			}
		}
		inputs = append(inputs, input)
	}
	if len(inputs) == 0 {
		return nil
	}
	return inputs
}

func ensureNodeContractInput(byBase map[string]*partialNodeContractInput, base string) *partialNodeContractInput {
	if byBase[base] == nil {
		byBase[base] = &partialNodeContractInput{}
	}
	return byBase[base]
}

func contractFirstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
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

// ── HandlePersister ───────────────────────────────────────────────────────────

// persistedRuntimeHandle is the JSON-serializable form of runtimeHandle.
// Both fields survive a round-trip through spec.NodeRecord.CurrentAttemptHandleJSON.
type persistedRuntimeHandle struct {
	AttemptHandle spruntime.AttemptHandle `json:"attempt_handle"`
	QueueName     string                  `json:"queue_name,omitempty"`
}

// MarshalHandle serializes a runtimeHandle for persistence.
// Returns (nil, nil) for handle types that do not support persistence.
func (a *SpawnerK8sAdapter) MarshalHandle(h Handle) ([]byte, error) {
	rh, ok := h.(runtimeHandle)
	if !ok {
		return nil, nil
	}
	return json.Marshal(persistedRuntimeHandle{
		AttemptHandle: rh.handle,
		QueueName:     rh.queueName,
	})
}

// UnmarshalHandle deserializes a runtimeHandle persisted by MarshalHandle.
func (a *SpawnerK8sAdapter) UnmarshalHandle(data []byte) (Handle, error) {
	var p persistedRuntimeHandle
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, err
	}
	return runtimeHandle{handle: p.AttemptHandle, queueName: p.QueueName}, nil
}

// ── Runtime path helpers ──────────────────────────────────────────────────────

// toAttemptRequest converts a JUMI spec.RunRecord + spec.Node into a spawner
// AttemptRequest. The node.Env must already contain JUMI_ATTEMPT_ID, JUMI_RUN_ID,
// and JUMI_NODE_ID (injected by the executor's injectRuntimeContextEnv before
// PrepareNode is called).
func toAttemptRequest(run spec.RunRecord, node spec.Node) spruntime.AttemptRequest {
	attemptID := node.Env["JUMI_ATTEMPT_ID"]

	env := cloneEnv(node.Env)
	injectOutputContractEnv(env, run, node)

	command := append(append([]string{}, node.Command...), node.Args...)
	command = wrapCommandForManifestExport(command, node)

	userLabels := make(map[string]string)
	if node.Kueue != nil {
		if node.Kueue.QueueName != "" {
			userLabels["kueue.x-k8s.io/queue-name"] = node.Kueue.QueueName
		}
		for k, v := range node.Kueue.Labels {
			if !strings.HasPrefix(k, "user.jumi.io/") {
				continue
			}
			userLabels[k] = v
		}
	}

	userAnnotations := make(map[string]string)
	for k, v := range node.Metadata {
		userAnnotations[k] = v
	}
	if run.Spec.Run.TraceID != "" {
		userAnnotations["jumi.trace-id"] = run.Spec.Run.TraceID
	}

	var placement *spruntime.Placement
	if node.Placement != nil {
		p := &spruntime.Placement{
			NodeSelector:     node.Placement.NodeSelector,
			RequiredNodeName: node.Placement.RequiredNodeName,
		}
		for _, pn := range node.Placement.PreferredNodes {
			p.PreferredNodes = append(p.PreferredNodes, spruntime.PreferredNode{
				NodeName: pn.NodeName,
				Weight:   pn.Weight,
			})
		}
		placement = p
	}

	mounts := make([]spruntime.Mount, 0, len(node.Mounts))
	for _, m := range node.Mounts {
		mounts = append(mounts, toRuntimeMount(m))
	}

	var timeout time.Duration
	if node.TimeoutPolicy.Seconds > 0 {
		timeout = time.Duration(node.TimeoutPolicy.Seconds) * time.Second
	}

	return spruntime.AttemptRequest{
		AttemptID:          attemptID,
		RunID:              run.RunID,
		CorrelationID:      run.Spec.Run.TraceID,
		ImageRef:           node.Image,
		Command:            command,
		WorkingDir:         node.WorkingDir,
		Env:                env,
		Resources:          spruntime.Resources{CPU: node.ResourceProfile.CPU, Memory: node.ResourceProfile.Memory},
		ServiceAccountName: node.ServiceAccountName,
		Mounts:             mounts,
		Placement:          placement,
		UserLabels:         userLabels,
		UserAnnotations:    userAnnotations,
		Cleanup:            spruntime.CleanupPolicy{TTLSecondsAfterFinished: node.CleanupPolicy.TTLSecondsAfterFinished},
		AttemptTimeout:     timeout,
	}
}

// toRuntimeMount converts a spec.Mount to a spruntime.Mount.
// The existing "hostpath:" source prefix convention is mapped to MountKindHostPath.
func toRuntimeMount(m spec.Mount) spruntime.Mount {
	source := m.Source
	kind := spruntime.MountKindPVC
	if strings.HasPrefix(source, "hostpath:") {
		kind = spruntime.MountKindHostPath
		source = strings.TrimPrefix(source, "hostpath:")
	}
	return spruntime.Mount{
		Kind:     kind,
		Source:   source,
		Target:   m.Target,
		ReadOnly: strings.EqualFold(m.Mode, "ro") || strings.EqualFold(m.Mode, "readonly"),
	}
}

// kueueQueueName extracts the Kueue queue name from a node spec, or "" if absent.
func kueueQueueName(node spec.Node) string {
	if node.Kueue != nil {
		return node.Kueue.QueueName
	}
	return ""
}

// runtimeReasonToFailure maps spawner Reason constants to JUMI terminal failure reasons.
func runtimeReasonToFailure(reason string) string {
	switch reason {
	case spruntime.ReasonOOMKilled:
		return "oom_killed"
	case spruntime.ReasonNodeLost:
		return "node_lost"
	case spruntime.ReasonBackoffExceeded:
		return "backoff_exceeded"
	case spruntime.ReasonDeadlineExceeded:
		return "deadline_exceeded"
	case spruntime.ReasonImagePullFailed:
		return "image_pull_failed"
	default:
		return "backend_wait_error"
	}
}
