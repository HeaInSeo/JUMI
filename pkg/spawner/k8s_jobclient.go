package spawner

import (
	"context"
	"fmt"
	"time"

	spruntime "github.com/HeaInSeo/spawner/pkg/runtime"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8swatch "k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
)

const labelAttemptMarker = "spawner.io/attempt-marker"

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
	if existing.Labels[labelAttemptMarker] != req.AttemptMarker {
		return spruntime.BackendRef{}, spruntime.ErrJobConflict
	}
	return spruntime.NewK8sJobBackendRef(req.Namespace, req.JobName, string(existing.UID)), nil
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
	propagation := metav1.DeletePropagationBackground
	err := c.clientset.BatchV1().Jobs(ref.Namespace).Delete(ctx, ref.Name, metav1.DeleteOptions{
		PropagationPolicy: &propagation,
	})
	if k8serrors.IsNotFound(err) {
		return nil
	}
	return err
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
		LabelSelector:   "job-name=" + ref.Name,
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
			if s, emit := podPhaseToState(pod); emit {
				if !trySendEvent(ctx, evCh, spruntime.JobEvent{State: s, Timestamp: time.Now()}) {
					return
				}
			}
		}
	}
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

	labels := make(map[string]string, len(r.UserLabels)+1)
	for k, v := range r.UserLabels {
		labels[k] = v
	}
	labels[labelAttemptMarker] = req.AttemptMarker

	annotations := make(map[string]string, len(r.UserAnnotations))
	for k, v := range r.UserAnnotations {
		annotations[k] = v
	}

	_, useKueue := labels["kueue.x-k8s.io/queue-name"]
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
					ServiceAccountName: r.ServiceAccountName,
					RestartPolicy:      corev1.RestartPolicyNever,
					Containers:         []corev1.Container{container},
					Volumes:            volumes,
					NodeSelector:       buildK8sNodeSelector(r.Placement),
					Affinity:           buildK8sAffinity(r.Placement),
				},
			},
		},
	}
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
		out["kubernetes.io/hostname"] = p.RequiredNodeName
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
