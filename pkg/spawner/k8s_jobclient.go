package spawner

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	spruntime "github.com/HeaInSeo/spawner/pkg/runtime"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8slabels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	k8svalidation "k8s.io/apimachinery/pkg/util/validation"
	k8swatch "k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
)

const (
	labelAppName         = "app.kubernetes.io/name"
	labelAppComponent    = "app.kubernetes.io/component"
	labelAppManagedBy    = "app.kubernetes.io/managed-by"
	labelRunKey          = "jumi.io/run-key"
	labelNodeKey         = "jumi.io/node-key"
	labelAttemptID       = "jumi.io/attempt-id"
	labelWorkloadRole    = "jumi.io/workload-role"
	labelKueueQueueName  = "kueue.x-k8s.io/queue-name"
	labelAttemptMarker   = "spawner.io/attempt-marker"
	annotationRunID      = "jumi.io/run-id"
	annotationNodeID     = "jumi.io/node-id"
	annotationAttemptID  = "jumi.io/attempt-id"
	annotationMarker     = "jumi.io/attempt-marker"
	annotationTraceID    = "jumi.io/trace-id"
	userLabelPrefix      = "user.jumi.io/"
	hostnameNodeSelector = "kubernetes.io/hostname"
)

const (
	defaultNanShutdownGracePeriodEnv        = "25s"
	defaultPodTerminationGracePeriodSeconds = int64(30)
	podTerminationGracePeriodMarginSeconds  = int64(5)
	nanShutdownGracePeriodEnv               = "JUMI_SHUTDOWN_GRACE_PERIOD"
)

// K8sJobClient implements spruntime.JobClient using the Kubernetes batch/v1 Job API.
// It is the JUMI-side implementation of the spawner Runtime/JobClient contract.
type K8sJobClient struct {
	clientset kubernetes.Interface
}

// NewK8sJobClient creates a K8sJobClient backed by the provided clientset.
func NewK8sJobClient(clientset kubernetes.Interface) *K8sJobClient {
	return &K8sJobClient{clientset: clientset}
}

// Create submits a new K8s Job from req. Idempotent via AttemptMarker:
// same (Namespace, JobName) + matching marker → return existing BackendRef;
// same (Namespace, JobName) + different marker → ErrJobConflict.
func (c *K8sJobClient) Create(ctx context.Context, req spruntime.JobCreateRequest) (spruntime.BackendRef, error) {
	if err := validateK8sJobCreateRequest(req); err != nil {
		return spruntime.BackendRef{}, err
	}
	job := buildK8sJob(req)
	created, err := c.clientset.BatchV1().Jobs(req.Namespace).Create(ctx, job, metav1.CreateOptions{})
	if err == nil {
		return spruntime.NewK8sJobBackendRef(req.Namespace, req.JobName, string(created.UID)), nil
	}
	if !k8serrors.IsAlreadyExists(err) {
		return spruntime.BackendRef{}, err
	}

	existing, getErr := c.clientset.BatchV1().Jobs(req.Namespace).Get(ctx, req.JobName, metav1.GetOptions{})
	if getErr != nil {
		return spruntime.BackendRef{}, fmt.Errorf("job AlreadyExists but get failed: %w", getErr)
	}
	if !existingAttemptMarkerMatches(existing, req.AttemptMarker) {
		return spruntime.BackendRef{}, spruntime.ErrJobConflict
	}
	return spruntime.NewK8sJobBackendRef(req.Namespace, req.JobName, string(existing.UID)), nil
}

func existingAttemptMarkerMatches(job *batchv1.Job, marker string) bool {
	if job == nil {
		return false
	}
	if job.Annotations[annotationMarker] == marker {
		return true
	}
	return job.Labels[labelAttemptMarker] == marker
}

// Watch starts a goroutine that translates K8s Job and Pod events into JobEvents.
// See JobWatch contract in jobclient.go.
func (c *K8sJobClient) Watch(ctx context.Context, ref spruntime.BackendRef) (spruntime.JobWatch, error) {
	evCh := make(chan spruntime.JobEvent, 16)
	errCh := make(chan spruntime.JobWatchError, 1)
	go c.watchJob(ctx, ref, evCh, errCh)
	return spruntime.JobWatch{Events: evCh, Errs: errCh}, nil
}

// Delete removes the K8s Job. NotFound is treated as success.
func (c *K8sJobClient) Delete(ctx context.Context, ref spruntime.BackendRef) error {
	if ref.UID != "" {
		job, err := c.clientset.BatchV1().Jobs(ref.Namespace).Get(ctx, ref.Name, metav1.GetOptions{})
		if k8serrors.IsNotFound(err) {
			return nil
		}
		if err != nil {
			return err
		}
		if string(job.UID) != ref.UID {
			return nil
		}
	}
	propagation := metav1.DeletePropagationBackground
	preconditions := deletePreconditions(ref)
	err := c.clientset.BatchV1().Jobs(ref.Namespace).Delete(ctx, ref.Name, metav1.DeleteOptions{
		PropagationPolicy: &propagation,
		Preconditions:     preconditions,
	})
	if k8serrors.IsNotFound(err) {
		return nil
	}
	return err
}

func deletePreconditions(ref spruntime.BackendRef) *metav1.Preconditions {
	if ref.UID == "" {
		return nil
	}
	uid := types.UID(ref.UID)
	return &metav1.Preconditions{UID: &uid}
}

// Snapshot returns the current state of the Job without subscribing to a watch stream.
// Returns Exists=false if the Job was not found.
func (c *K8sJobClient) Snapshot(ctx context.Context, ref spruntime.BackendRef) (spruntime.JobSnapshot, error) {
	job, err := c.clientset.BatchV1().Jobs(ref.Namespace).Get(ctx, ref.Name, metav1.GetOptions{})
	if k8serrors.IsNotFound(err) {
		return spruntime.JobSnapshot{Exists: false}, nil
	}
	if err != nil {
		return spruntime.JobSnapshot{}, err
	}
	if !jobMatchesBackendRef(job, ref) {
		return spruntime.JobSnapshot{Exists: false}, nil
	}
	state, reason, msg, ts := jobToState(job)
	return spruntime.JobSnapshot{
		Exists:    true,
		State:     state,
		Reason:    reason,
		Message:   msg,
		Timestamp: ts,
	}, nil
}

// watchJob is the goroutine body for Watch. It drives a K8s Watch event loop on
// the Job and its Pods, translating K8s events into JobEvents.
//
// It performs an initial Get to seed the ResourceVersion, then starts both a
// Job watch and a Pod watch. The Pod watch enables Pending→Running detection
// because job.status.active > 0 doesn't distinguish between pending and running pods.
func (c *K8sJobClient) watchJob(ctx context.Context, ref spruntime.BackendRef, evCh chan<- spruntime.JobEvent, errCh chan<- spruntime.JobWatchError) {
	defer close(evCh)
	defer close(errCh)

	job, err := c.clientset.BatchV1().Jobs(ref.Namespace).Get(ctx, ref.Name, metav1.GetOptions{})
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return // job not found: close channels immediately per contract
		}
		trySendWatchErr(errCh, spruntime.ReasonWatchDisconnected, err.Error(), true)
		return
	}
	if !jobMatchesBackendRef(job, ref) {
		return
	}

	// If already terminal, emit once and close.
	if state, reason, msg, ts := jobToState(job); state.IsTerminal() {
		trySendEvent(ctx, evCh, spruntime.JobEvent{State: state, Reason: reason, Message: msg, Timestamp: ts})
		return
	}

	var watchTimeoutSec int64 = 300
	jw, err := c.clientset.BatchV1().Jobs(ref.Namespace).Watch(ctx, metav1.ListOptions{
		FieldSelector:   "metadata.name=" + ref.Name,
		ResourceVersion: job.ResourceVersion,
		TimeoutSeconds:  &watchTimeoutSec,
	})
	if err != nil {
		trySendWatchErr(errCh, spruntime.ReasonWatchDisconnected, err.Error(), true)
		return
	}
	defer jw.Stop()

	pw, pwErr := c.clientset.CoreV1().Pods(ref.Namespace).Watch(ctx, metav1.ListOptions{
		LabelSelector:   podWatchLabelSelector(job, ref),
		ResourceVersion: "0",
		TimeoutSeconds:  &watchTimeoutSec,
	})
	var podCh <-chan k8swatch.Event
	if pwErr == nil {
		defer pw.Stop()
		podCh = pw.ResultChan()
	}

	for {
		select {
		case <-ctx.Done():
			return

		case ev, ok := <-jw.ResultChan():
			if !ok {
				trySendWatchErr(errCh, spruntime.ReasonWatchDisconnected, "watch stream closed", true)
				return
			}
			switch ev.Type {
			case k8swatch.Deleted:
				trySendEvent(ctx, evCh, spruntime.JobEvent{
					State:     spruntime.AttemptStateFailed,
					Reason:    spruntime.ReasonSystemCancel,
					Message:   "job deleted externally",
					Timestamp: time.Now(),
				})
				return
			case k8swatch.Error:
				if status, ok := ev.Object.(*metav1.Status); ok && status.Code == 410 {
					trySendWatchErr(errCh, spruntime.ReasonResourceVersionTooOld, "resource version too old (410)", true)
				} else {
					trySendWatchErr(errCh, spruntime.ReasonWatchDisconnected, "watch error event", true)
				}
				return
			}
			j, ok := ev.Object.(*batchv1.Job)
			if !ok {
				continue
			}
			if !jobMatchesBackendRef(j, ref) || !jobMatchesIdentityLabels(j, job.Labels) {
				continue
			}
			state, reason, msg, ts := jobToState(j)
			if !trySendEvent(ctx, evCh, spruntime.JobEvent{State: state, Reason: reason, Message: msg, Timestamp: ts}) {
				return
			}
			if state.IsTerminal() {
				return
			}

		case pev, ok := <-podCh:
			if !ok {
				podCh = nil
				continue
			}
			if pev.Type == k8swatch.Deleted {
				continue
			}
			pod, ok := pev.Object.(*corev1.Pod)
			if !ok {
				continue
			}
			if !podMatchesIdentityLabels(pod, job.Labels) {
				continue
			}
			if s, emit := podPhaseToState(pod); emit {
				if !trySendEvent(ctx, evCh, spruntime.JobEvent{State: s, Timestamp: time.Now()}) {
					return
				}
			}
		}
	}
}

func jobMatchesBackendRef(job *batchv1.Job, ref spruntime.BackendRef) bool {
	if job == nil {
		return false
	}
	return ref.UID == "" || string(job.UID) == ref.UID
}

func jobMatchesIdentityLabels(job *batchv1.Job, expected map[string]string) bool {
	if job == nil {
		return false
	}
	return metadataMatchesIdentityLabels(job.Labels, expected)
}

func podMatchesIdentityLabels(pod *corev1.Pod, expected map[string]string) bool {
	if pod == nil {
		return false
	}
	return metadataMatchesIdentityLabels(pod.Labels, expected)
}

func metadataMatchesIdentityLabels(got, expected map[string]string) bool {
	for _, key := range []string{labelRunKey, labelNodeKey, labelAttemptID} {
		want := expected[key]
		if want == "" {
			continue
		}
		if got[key] != want {
			return false
		}
	}
	return true
}

func podWatchLabelSelector(job *batchv1.Job, ref spruntime.BackendRef) string {
	selector := map[string]string{"job-name": ref.Name}
	if job != nil {
		for _, key := range []string{labelRunKey, labelNodeKey, labelAttemptID} {
			if value := job.Labels[key]; value != "" {
				selector[key] = value
			}
		}
	}
	return k8slabels.Set(selector).String()
}

// jobToState translates a K8s Job object to an AttemptState.
//
// Priority order:
//  1. JobComplete/JobFailed conditions → terminal
//  2. spec.suspend=true → Submitted (Kueue admission pending)
//  3. Otherwise → Pending (pod watch provides Running upgrade)
func jobToState(job *batchv1.Job) (spruntime.AttemptState, string, string, time.Time) {
	for _, c := range job.Status.Conditions {
		if c.Type == batchv1.JobComplete && c.Status == corev1.ConditionTrue {
			return spruntime.AttemptStateSucceeded, "", c.Message, c.LastTransitionTime.Time
		}
	}
	for _, c := range job.Status.Conditions {
		if c.Type == batchv1.JobFailed && c.Status == corev1.ConditionTrue {
			return spruntime.AttemptStateFailed, mapJobFailureReason(c.Reason), c.Message, c.LastTransitionTime.Time
		}
	}
	if job.Spec.Suspend != nil && *job.Spec.Suspend {
		return spruntime.AttemptStateSubmitted, spruntime.ReasonKueueWaiting, "", time.Now()
	}
	return spruntime.AttemptStatePending, "", "", time.Now()
}

// podPhaseToState maps pod phase to an AttemptState for intermediate-state detection.
// Returns (state, true) only for phases that are worth emitting.
func podPhaseToState(pod *corev1.Pod) (spruntime.AttemptState, bool) {
	switch pod.Status.Phase {
	case corev1.PodRunning:
		return spruntime.AttemptStateRunning, true
	case corev1.PodPending:
		return spruntime.AttemptStatePending, true
	default:
		return "", false
	}
}

func mapJobFailureReason(k8sReason string) string {
	switch k8sReason {
	case "BackoffLimitExceeded":
		return spruntime.ReasonBackoffExceeded
	case "DeadlineExceeded":
		return spruntime.ReasonDeadlineExceeded
	default:
		return k8sReason
	}
}

// buildK8sJob constructs a batchv1.Job from a JobCreateRequest.
// The spawner.io/attempt-marker label is always written for idempotency checks.
func buildK8sJob(req spruntime.JobCreateRequest) *batchv1.Job {
	r := req.AttemptRequest

	container := corev1.Container{
		Name:            "main",
		Image:           r.ImageRef,
		Command:         r.Command,
		WorkingDir:      r.WorkingDir,
		Env:             buildEnvVars(r.Env),
		Resources:       buildK8sResources(r.Resources),
		ImagePullPolicy: corev1.PullIfNotPresent,
	}
	volumes, volumeMounts := buildK8sVolumes(r.Mounts)
	container.VolumeMounts = volumeMounts

	labels := buildLabels(req)
	annotations := make(map[string]string, len(r.UserAnnotations)+5)
	for k, v := range r.UserAnnotations {
		annotations[k] = v
	}
	annotations[annotationRunID] = firstNonEmpty(r.RunID, r.Env["JUMI_RUN_ID"])
	annotations[annotationNodeID] = r.Env["JUMI_NODE_ID"]
	annotations[annotationAttemptID] = r.AttemptID
	annotations[annotationMarker] = req.AttemptMarker
	if r.CorrelationID != "" {
		annotations[annotationTraceID] = r.CorrelationID
	}

	_, useKueue := labels[labelKueueQueueName]
	suspend := useKueue
	backoffLimit := int32(0)

	var ttl *int32
	if r.Cleanup.TTLSecondsAfterFinished > 0 {
		v := r.Cleanup.TTLSecondsAfterFinished
		ttl = &v
	}

	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:        req.JobName,
			Namespace:   req.Namespace,
			Labels:      labels,
			Annotations: annotations,
		},
		Spec: batchv1.JobSpec{
			Suspend:                 &suspend,
			BackoffLimit:            &backoffLimit,
			TTLSecondsAfterFinished: ttl,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels, Annotations: annotations},
				Spec: corev1.PodSpec{
					ServiceAccountName:            r.ServiceAccountName,
					RestartPolicy:                 corev1.RestartPolicyNever,
					TerminationGracePeriodSeconds: ptrInt64(podTerminationGracePeriodSeconds(r.Env)),
					Containers:                    []corev1.Container{container},
					Volumes:                       volumes,
					NodeSelector:                  buildK8sNodeSelector(r.Placement),
					Affinity:                      buildK8sAffinity(r.Placement),
				},
			},
		},
	}
}

func validateK8sJobCreateRequest(req spruntime.JobCreateRequest) error {
	r := req.AttemptRequest
	if strings.TrimSpace(r.AttemptID) == "" {
		return fmt.Errorf("attempt-id is required")
	}
	if strings.TrimSpace(firstNonEmpty(r.RunID, r.Env["JUMI_RUN_ID"])) == "" {
		return fmt.Errorf("run id is required")
	}
	if strings.TrimSpace(r.Env["JUMI_NODE_ID"]) == "" {
		return fmt.Errorf("node id is required")
	}
	if err := validatePlacement(r.Placement); err != nil {
		return err
	}
	for k, v := range r.UserLabels {
		if k == labelKueueQueueName {
			if errs := k8svalidation.IsValidLabelValue(v); len(errs) > 0 {
				return fmt.Errorf("invalid kueue queue label value %q: %s", v, strings.Join(errs, "; "))
			}
			continue
		}
		if !strings.HasPrefix(k, userLabelPrefix) {
			return fmt.Errorf("user label %q must use %s prefix", k, userLabelPrefix)
		}
		if err := validateLabel(k, v); err != nil {
			return err
		}
	}
	labels := buildLabels(req)
	for k, v := range labels {
		if err := validateLabel(k, v); err != nil {
			return err
		}
	}
	return nil
}

func validatePlacement(p *spruntime.Placement) error {
	if p == nil || p.RequiredNodeName == "" || p.NodeSelector == nil {
		return nil
	}
	if existing := strings.TrimSpace(p.NodeSelector[hostnameNodeSelector]); existing != "" && existing != p.RequiredNodeName {
		return fmt.Errorf("requiredNodeName %q conflicts with nodeSelector[%q]=%q", p.RequiredNodeName, hostnameNodeSelector, existing)
	}
	return nil
}

func validateLabel(k, v string) error {
	if errs := k8svalidation.IsQualifiedName(k); len(errs) > 0 {
		return fmt.Errorf("invalid Kubernetes label key %q: %s", k, strings.Join(errs, "; "))
	}
	if errs := k8svalidation.IsValidLabelValue(v); len(errs) > 0 {
		return fmt.Errorf("invalid Kubernetes label value for %q: %s", k, strings.Join(errs, "; "))
	}
	return nil
}

func buildLabels(req spruntime.JobCreateRequest) map[string]string {
	r := req.AttemptRequest
	labels := make(map[string]string, len(r.UserLabels)+8)
	labels[labelAppName] = "jumi"
	labels[labelAppComponent] = "node-attempt"
	labels[labelAppManagedBy] = "jumi-spawner"
	labels[labelRunKey] = identityLabelValue(firstNonEmpty(r.RunID, r.Env["JUMI_RUN_ID"]))
	labels[labelNodeKey] = identityLabelValue(r.Env["JUMI_NODE_ID"])
	labels[labelAttemptID] = identityLabelValue(r.AttemptID)
	labels[labelWorkloadRole] = "main"
	labels[labelAttemptMarker] = identityLabelValue(req.AttemptMarker)
	for k, v := range r.UserLabels {
		if k == labelKueueQueueName || strings.HasPrefix(k, userLabelPrefix) {
			labels[k] = v
		}
	}
	return labels
}

func identityLabelValue(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "unknown"
	}
	lower := strings.ToLower(raw)
	sanitized := sanitizeLabelValue(lower)
	if sanitized == "" {
		sanitized = "x"
	}
	if sanitized == lower && len(sanitized) <= 63 {
		return sanitized
	}
	hash := shortHash(raw)
	maxPrefix := 63 - len(hash) - 1
	if len(sanitized) > maxPrefix {
		sanitized = sanitized[:maxPrefix]
		sanitized = strings.Trim(sanitized, "-_.")
	}
	if sanitized == "" {
		return hash
	}
	return sanitized + "-" + hash
}

func sanitizeLabelValue(raw string) string {
	var b strings.Builder
	lastDash := false
	for _, r := range raw {
		allowed := isASCIILabelChar(r)
		if allowed {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			b.WriteByte('-')
			lastDash = true
		}
	}
	return strings.Trim(b.String(), "-_.")
}

func isASCIILabelChar(r rune) bool {
	return r == '-' || r == '_' || r == '.' ||
		('a' <= r && r <= 'z') ||
		('A' <= r && r <= 'Z') ||
		('0' <= r && r <= '9')
}

func shortHash(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])[:8]
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func ptrInt64(v int64) *int64 {
	return &v
}

func podTerminationGracePeriodSeconds(env map[string]string) int64 {
	raw := defaultNanShutdownGracePeriodEnv
	if configured := env[nanShutdownGracePeriodEnv]; configured != "" {
		raw = configured
	}
	grace, err := time.ParseDuration(raw)
	if err != nil || grace <= 0 {
		return defaultPodTerminationGracePeriodSeconds
	}
	seconds := int64(grace / time.Second)
	if grace%time.Second != 0 {
		seconds++
	}
	return seconds + podTerminationGracePeriodMarginSeconds
}

func buildEnvVars(env map[string]string) []corev1.EnvVar {
	if len(env) == 0 {
		return nil
	}
	vars := make([]corev1.EnvVar, 0, len(env))
	for k, v := range env {
		vars = append(vars, corev1.EnvVar{Name: k, Value: v})
	}
	return vars
}

func buildK8sResources(r spruntime.Resources) corev1.ResourceRequirements {
	req := corev1.ResourceList{}
	lim := corev1.ResourceList{}
	if r.CPU != "" {
		if q, err := resource.ParseQuantity(r.CPU); err == nil {
			req[corev1.ResourceCPU] = q
			lim[corev1.ResourceCPU] = q
		}
	}
	if r.Memory != "" {
		if q, err := resource.ParseQuantity(r.Memory); err == nil {
			req[corev1.ResourceMemory] = q
			lim[corev1.ResourceMemory] = q
		}
	}
	return corev1.ResourceRequirements{Requests: req, Limits: lim}
}

func buildK8sVolumes(mounts []spruntime.Mount) ([]corev1.Volume, []corev1.VolumeMount) {
	vols := make([]corev1.Volume, 0, len(mounts))
	vmounts := make([]corev1.VolumeMount, 0, len(mounts))
	for i, m := range mounts {
		name := fmt.Sprintf("vol-%d", i)
		var vs corev1.VolumeSource
		switch m.Kind {
		case spruntime.MountKindHostPath:
			hpType := corev1.HostPathDirectoryOrCreate
			vs = corev1.VolumeSource{
				HostPath: &corev1.HostPathVolumeSource{Path: m.Source, Type: &hpType},
			}
		default: // MountKindPVC and any future kinds default to PVC
			vs = corev1.VolumeSource{
				PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
					ClaimName: m.Source,
					ReadOnly:  m.ReadOnly,
				},
			}
		}
		vols = append(vols, corev1.Volume{Name: name, VolumeSource: vs})
		vmounts = append(vmounts, corev1.VolumeMount{
			Name:      name,
			MountPath: m.Target,
			ReadOnly:  m.ReadOnly,
		})
	}
	return vols, vmounts
}

func buildK8sNodeSelector(p *spruntime.Placement) map[string]string {
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
		out[hostnameNodeSelector] = p.RequiredNodeName
	}
	return out
}

func buildK8sAffinity(p *spruntime.Placement) *corev1.Affinity {
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

// trySendEvent sends ev to ch, returning false if ctx is cancelled first.
func trySendEvent(ctx context.Context, ch chan<- spruntime.JobEvent, ev spruntime.JobEvent) bool {
	select {
	case ch <- ev:
		return true
	case <-ctx.Done():
		return false
	}
}

// trySendWatchErr sends a JobWatchError to errCh non-blocking (capacity-1 buffer).
func trySendWatchErr(ch chan<- spruntime.JobWatchError, reason, msg string, temporary bool) {
	select {
	case ch <- spruntime.JobWatchError{Reason: reason, Message: msg, Temporary: temporary}:
	default:
	}
}
