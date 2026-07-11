package spawner

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	spruntime "github.com/HeaInSeo/spawner/pkg/runtime"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
	"sigs.k8s.io/yaml"
)

func TestBuildK8sJobSetsTerminationGracePeriod(t *testing.T) {
	job := buildK8sJob(spruntime.JobCreateRequest{
		AttemptRequest: spruntime.AttemptRequest{
			AttemptID: "attempt-1",
			RunID:     "run-1",
			ImageRef:  "tool:latest",
			Command:   []string{"true"},
		},
		JobName:       "job-1",
		Namespace:     "default",
		AttemptMarker: "marker-1",
	})
	got := job.Spec.Template.Spec.TerminationGracePeriodSeconds
	if got == nil {
		t.Fatal("TerminationGracePeriodSeconds = nil, want default")
	}
	if *got != defaultPodTerminationGracePeriodSeconds {
		t.Fatalf("TerminationGracePeriodSeconds = %d, want %d", *got, defaultPodTerminationGracePeriodSeconds)
	}
}

func TestBuildK8sJobSetsTerminationGracePeriodAboveNanGrace(t *testing.T) {
	job := buildK8sJob(spruntime.JobCreateRequest{
		AttemptRequest: spruntime.AttemptRequest{
			AttemptID: "attempt-1",
			RunID:     "run-1",
			ImageRef:  "tool:latest",
			Command:   []string{"true"},
			Env:       map[string]string{"JUMI_SHUTDOWN_GRACE_PERIOD": "60s"},
		},
		JobName:       "job-1",
		Namespace:     "default",
		AttemptMarker: "marker-1",
	})
	got := job.Spec.Template.Spec.TerminationGracePeriodSeconds
	if got == nil {
		t.Fatal("TerminationGracePeriodSeconds = nil, want computed value")
	}
	want := int64(65)
	if *got != want {
		t.Fatalf("TerminationGracePeriodSeconds = %d, want %d", *got, want)
	}
}

func TestPodTerminationGracePeriodFallsBackOnInvalidNanGrace(t *testing.T) {
	got := podTerminationGracePeriodSeconds(map[string]string{"JUMI_SHUTDOWN_GRACE_PERIOD": "invalid"})
	if got != defaultPodTerminationGracePeriodSeconds {
		t.Fatalf("podTerminationGracePeriodSeconds() = %d, want %d", got, defaultPodTerminationGracePeriodSeconds)
	}
}

func TestBuildK8sJobAppliesIdentityLabelContract(t *testing.T) {
	job := buildK8sJob(spruntime.JobCreateRequest{
		AttemptRequest: spruntime.AttemptRequest{
			AttemptID:     "attempt-0002",
			RunID:         "run-1",
			CorrelationID: "trace-1",
			ImageRef:      "tool:latest",
			Command:       []string{"true"},
			Env: map[string]string{
				"JUMI_RUN_ID":     "run-1",
				"JUMI_NODE_ID":    "sort/bam after bwa",
				"JUMI_ATTEMPT_ID": "attempt-0002",
			},
			UserLabels: map[string]string{
				"user.jumi.io/team": "genomics",
			},
		},
		JobName:       "job-1",
		Namespace:     "default",
		AttemptMarker: "marker-1",
	})

	for _, labels := range []map[string]string{job.Labels, job.Spec.Template.Labels} {
		if labels[labelAppName] != "jumi" {
			t.Fatalf("app name label = %q, want jumi", labels[labelAppName])
		}
		if labels[labelRunKey] != "run-1" {
			t.Fatalf("run-key = %q, want run-1", labels[labelRunKey])
		}
		if labels[labelNodeKey] == "" || !strings.HasPrefix(labels[labelNodeKey], "sort-bam-after-bwa-") {
			t.Fatalf("node-key = %q, want sanitized value with hash", labels[labelNodeKey])
		}
		if labels[labelAttemptID] != "attempt-0002" {
			t.Fatalf("attempt-id = %q, want attempt-0002", labels[labelAttemptID])
		}
		if labels[labelWorkloadRole] != "main" {
			t.Fatalf("workload-role = %q, want main", labels[labelWorkloadRole])
		}
		if labels[labelAttemptMarker] != "marker-1" {
			t.Fatalf("attempt marker label = %q, want marker-1", labels[labelAttemptMarker])
		}
		if labels["user.jumi.io/team"] != "genomics" {
			t.Fatalf("user label = %q, want genomics", labels["user.jumi.io/team"])
		}
	}
	if job.Annotations[annotationRunID] != "run-1" {
		t.Fatalf("run-id annotation = %q, want run-1", job.Annotations[annotationRunID])
	}
	if job.Annotations[annotationNodeID] != "sort/bam after bwa" {
		t.Fatalf("node-id annotation = %q, want original node id", job.Annotations[annotationNodeID])
	}
	if job.Annotations[annotationMarker] != "marker-1" {
		t.Fatalf("attempt-marker annotation = %q, want marker-1", job.Annotations[annotationMarker])
	}
	if job.Spec.Template.Annotations[annotationTraceID] != "trace-1" {
		t.Fatalf("trace-id pod annotation = %q, want trace-1", job.Spec.Template.Annotations[annotationTraceID])
	}
}

func TestRenderedK8sJobGoldenFixtures(t *testing.T) {
	tests := []struct {
		name string
		path string
		req  spruntime.JobCreateRequest
	}{
		{
			name: "preferred remote_fetch kueue",
			path: "k8s-job-preferred-remote-fetch-kueue.golden.yaml",
			req:  preferredRemoteFetchKueueJobRequest(),
		},
		{
			name: "required local_reuse",
			path: "k8s-job-required-local-reuse.golden.yaml",
			req:  requiredLocalReuseJobRequest(),
		},
		{
			name: "plain non-kueue",
			path: "k8s-job-plain-non-kueue.golden.yaml",
			req:  plainNonKueueJobRequest(),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertRenderedK8sJobGolden(t, tt.path, buildK8sJob(tt.req))
		})
	}
}

func assertRenderedK8sJobGolden(t *testing.T, filename string, job *batchv1.Job) {
	t.Helper()
	rendered, err := yaml.Marshal(job)
	if err != nil {
		t.Fatalf("marshal job fixture: %v", err)
	}
	path := filepath.Join("testdata", filename)
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.WriteFile(path, rendered, 0o644); err != nil {
			t.Fatalf("update golden fixture: %v", err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden fixture: %v", err)
	}
	if string(rendered) != string(want) {
		t.Fatalf("rendered Job YAML drifted from %s\n\nUpdate intentionally with:\nUPDATE_GOLDEN=1 go test ./pkg/spawner -run TestRenderedK8sJobGoldenFixtures", path)
	}
}

func preferredRemoteFetchKueueJobRequest() spruntime.JobCreateRequest {
	return spruntime.JobCreateRequest{
		AttemptRequest: spruntime.AttemptRequest{
			AttemptID:          "attempt-0002",
			RunID:              "run-2026-07-09-8c1f9c4a9d2e",
			CorrelationID:      "trace-7f9c4d2a",
			ImageRef:           "registry.example.com/jumi/samtools@sha256:abc",
			Command:            []string{"samtools", "sort", "/input/aligned.bam", "-o", "/output/sorted.bam"},
			ServiceAccountName: "jumi-node-runner",
			Env: map[string]string{
				"JUMI_RUN_ID":                                     "run-2026-07-09-8c1f9c4a9d2e",
				"JUMI_NODE_ID":                                    "sort-bam-after-bwa-align",
				"JUMI_ATTEMPT_ID":                                 "attempt-0002",
				"JUMI_INPUT_ALIGNED_BAM_DECISION":                 "remote_fetch",
				"JUMI_INPUT_ALIGNED_BAM_EXPECTED_DIGEST":          "sha256:abc",
				"JUMI_INPUT_ALIGNED_BAM_EXPECTED_SIZE_BYTES":      "128849018880",
				"JUMI_INPUT_ALIGNED_BAM_LOCAL_PATH":               "inputs/aligned.bam",
				"JUMI_INPUT_ALIGNED_BAM_MATERIALIZATION_MODE":     "remote_fetch",
				"JUMI_INPUT_ALIGNED_BAM_PLACEMENT_MODE":           "preferred_node",
				"JUMI_INPUT_ALIGNED_BAM_REQUIRES_MATERIALIZATION": "true",
				"JUMI_INPUT_ALIGNED_BAM_SOURCE_NODE":              "worker-1",
				"JUMI_INPUT_ALIGNED_BAM_URI":                      "http://artifact.local/aligned.bam",
			},
			Resources: spruntime.Resources{CPU: "2", Memory: "4Gi"},
			Mounts: []spruntime.Mount{
				{Kind: spruntime.MountKindPVC, Source: "jumi-run01-input", Target: "/input", ReadOnly: true},
				{Kind: spruntime.MountKindPVC, Source: "jumi-run01-output", Target: "/output"},
			},
			Placement: &spruntime.Placement{
				NodeSelector: map[string]string{"disk": "nvme"},
				PreferredNodes: []spruntime.PreferredNode{
					{NodeName: "worker-1", Weight: 100},
				},
			},
			UserLabels: map[string]string{
				labelKueueQueueName:    "gpu-batch",
				"user.jumi.io/project": "cancer-pipeline",
				"user.jumi.io/team":    "genomics",
			},
			Cleanup: spruntime.CleanupPolicy{TTLSecondsAfterFinished: 3600},
		},
		JobName:       "jumi-run01-node-sortbam-attempt-0002",
		Namespace:     "jumi-runs",
		AttemptMarker: "attempt-0002-9f31a7",
	}
}

func requiredLocalReuseJobRequest() spruntime.JobCreateRequest {
	return spruntime.JobCreateRequest{
		AttemptRequest: spruntime.AttemptRequest{
			AttemptID:          "attempt-0003",
			RunID:              "run-local-reuse-2026-07-11",
			CorrelationID:      "trace-local-reuse",
			ImageRef:           "registry.example.com/jumi/bam-indexer@sha256:def",
			Command:            []string{"nan", "run", "--", "samtools", "index", "/work/inputs/result.bam"},
			ServiceAccountName: "jumi-node-runner",
			Env: map[string]string{
				"JUMI_RUN_ID":                                "run-local-reuse-2026-07-11",
				"JUMI_NODE_ID":                               "index-bam",
				"JUMI_ATTEMPT_ID":                            "attempt-0003",
				"JUMI_INPUT_RESULT_DECISION":                 "local_reuse",
				"JUMI_INPUT_RESULT_EXPECTED_DIGEST":          "sha256:def",
				"JUMI_INPUT_RESULT_EXPECTED_SIZE_BYTES":      "4294967296",
				"JUMI_INPUT_RESULT_LOCAL_PATH":               "inputs/result.bam",
				"JUMI_INPUT_RESULT_MATERIALIZATION_MODE":     "local_reuse",
				"JUMI_INPUT_RESULT_NODE_LOCAL_PATH":          "/var/lib/jumi-artifacts/cas/sha256/def",
				"JUMI_INPUT_RESULT_PLACEMENT_MODE":           "required_node",
				"JUMI_INPUT_RESULT_REQUIRES_MATERIALIZATION": "true",
				"JUMI_INPUT_RESULT_SOURCE_NODE":              "worker-1",
			},
			Resources: spruntime.Resources{CPU: "1", Memory: "2Gi"},
			Mounts: []spruntime.Mount{
				{Kind: spruntime.MountKindPVC, Source: "jumi-run01-output", Target: "/output"},
			},
			Placement: &spruntime.Placement{
				RequiredNodeName: "worker-1",
				NodeSelector:     map[string]string{"disk": "nvme"},
			},
			UserLabels: map[string]string{
				"user.jumi.io/project": "cancer-pipeline",
				"user.jumi.io/team":    "genomics",
			},
			Cleanup: spruntime.CleanupPolicy{TTLSecondsAfterFinished: 1800},
		},
		JobName:       "jumi-run01-node-indexbam-attempt-0003",
		Namespace:     "jumi-runs",
		AttemptMarker: "attempt-0003-a1b2c3",
	}
}

func plainNonKueueJobRequest() spruntime.JobCreateRequest {
	return spruntime.JobCreateRequest{
		AttemptRequest: spruntime.AttemptRequest{
			AttemptID:          "attempt-0001",
			RunID:              "run-plain-2026-07-11",
			ImageRef:           "registry.example.com/jumi/fastqc@sha256:123",
			Command:            []string{"fastqc", "/input/sample.fastq", "-o", "/output"},
			ServiceAccountName: "jumi-node-runner",
			Env: map[string]string{
				"JUMI_RUN_ID":     "run-plain-2026-07-11",
				"JUMI_NODE_ID":    "fastqc",
				"JUMI_ATTEMPT_ID": "attempt-0001",
			},
			Resources: spruntime.Resources{CPU: "500m", Memory: "1Gi"},
			Mounts: []spruntime.Mount{
				{Kind: spruntime.MountKindPVC, Source: "jumi-run01-input", Target: "/input", ReadOnly: true},
				{Kind: spruntime.MountKindPVC, Source: "jumi-run01-output", Target: "/output"},
			},
			UserLabels: map[string]string{
				"user.jumi.io/team": "genomics",
			},
			Cleanup: spruntime.CleanupPolicy{TTLSecondsAfterFinished: 600},
		},
		JobName:       "jumi-run01-node-fastqc-attempt-0001",
		Namespace:     "jumi-runs",
		AttemptMarker: "attempt-0001-plain",
	}
}

func TestBuildK8sJobKueueQueueSuspendsJob(t *testing.T) {
	job := buildK8sJob(spruntime.JobCreateRequest{
		AttemptRequest: spruntime.AttemptRequest{
			AttemptID: "attempt-1",
			RunID:     "run-1",
			ImageRef:  "tool:latest",
			Command:   []string{"true"},
			Env:       map[string]string{"JUMI_NODE_ID": "node-1"},
			UserLabels: map[string]string{
				labelKueueQueueName: "gpu-batch",
			},
		},
		JobName:       "job-1",
		Namespace:     "default",
		AttemptMarker: "marker-1",
	})
	if job.Labels[labelKueueQueueName] != "gpu-batch" {
		t.Fatalf("kueue queue label = %q, want gpu-batch", job.Labels[labelKueueQueueName])
	}
	if job.Spec.Suspend == nil || !*job.Spec.Suspend {
		t.Fatal("Suspend = false, want true for Kueue queue")
	}
}

func TestValidateK8sJobCreateRequestRejectsReservedUserLabel(t *testing.T) {
	err := validateK8sJobCreateRequest(spruntime.JobCreateRequest{
		AttemptRequest: spruntime.AttemptRequest{
			AttemptID: "attempt-1",
			RunID:     "run-1",
			ImageRef:  "tool:latest",
			Command:   []string{"true"},
			Env:       map[string]string{"JUMI_NODE_ID": "node-1"},
			UserLabels: map[string]string{
				"jumi.io/run-key": "override",
			},
		},
		JobName:       "job-1",
		Namespace:     "default",
		AttemptMarker: "marker-1",
	})
	if err == nil {
		t.Fatal("validateK8sJobCreateRequest() error = nil, want reserved label rejection")
	}
}

func TestValidateK8sJobCreateRequestRejectsRequiredNodeConflict(t *testing.T) {
	err := validateK8sJobCreateRequest(spruntime.JobCreateRequest{
		AttemptRequest: spruntime.AttemptRequest{
			AttemptID: "attempt-1",
			RunID:     "run-1",
			ImageRef:  "tool:latest",
			Command:   []string{"true"},
			Env:       map[string]string{"JUMI_NODE_ID": "node-1"},
			Placement: &spruntime.Placement{
				RequiredNodeName: "worker-1",
				NodeSelector:     map[string]string{hostnameNodeSelector: "worker-2"},
			},
		},
		JobName:       "job-1",
		Namespace:     "default",
		AttemptMarker: "marker-1",
	})
	if err == nil {
		t.Fatal("validateK8sJobCreateRequest() error = nil, want placement conflict")
	}
}

func TestBuildK8sJobStoresFullAttemptMarkerInAnnotation(t *testing.T) {
	marker := strings.Repeat("a", 64)
	job := buildK8sJob(spruntime.JobCreateRequest{
		AttemptRequest: spruntime.AttemptRequest{
			AttemptID: "attempt-1",
			RunID:     "run-1",
			ImageRef:  "tool:latest",
			Command:   []string{"true"},
			Env:       map[string]string{"JUMI_NODE_ID": "node-1"},
		},
		JobName:       "job-1",
		Namespace:     "default",
		AttemptMarker: marker,
	})
	if job.Annotations[annotationMarker] != marker {
		t.Fatalf("attempt marker annotation = %q, want full marker", job.Annotations[annotationMarker])
	}
	if got := job.Labels[labelAttemptMarker]; got == marker || len(got) > 63 {
		t.Fatalf("attempt marker label = %q, want shortened Kubernetes label value", got)
	}
	if err := validateK8sJobCreateRequest(spruntime.JobCreateRequest{
		AttemptRequest: spruntime.AttemptRequest{
			AttemptID: "attempt-1",
			RunID:     "run-1",
			ImageRef:  "tool:latest",
			Command:   []string{"true"},
			Env:       map[string]string{"JUMI_NODE_ID": "node-1"},
		},
		JobName:       "job-1",
		Namespace:     "default",
		AttemptMarker: marker,
	}); err != nil {
		t.Fatalf("validateK8sJobCreateRequest() error = %v", err)
	}
}

func TestExistingAttemptMarkerMatchesAnnotationBeforeLegacyLabel(t *testing.T) {
	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Labels:      map[string]string{labelAttemptMarker: "short"},
			Annotations: map[string]string{annotationMarker: strings.Repeat("a", 64)},
		},
	}
	if !existingAttemptMarkerMatches(job, strings.Repeat("a", 64)) {
		t.Fatal("existingAttemptMarkerMatches() = false, want true from annotation")
	}
	if existingAttemptMarkerMatches(job, strings.Repeat("b", 64)) {
		t.Fatal("existingAttemptMarkerMatches() = true, want false for different marker")
	}
}

func TestIdentityLabelValueHandlesNonASCII(t *testing.T) {
	got := identityLabelValue("샘플/노드")
	if got == "" || len(got) > 63 {
		t.Fatalf("identityLabelValue() = %q, want non-empty value within label limit", got)
	}
	if err := validateLabel(labelNodeKey, got); err != nil {
		t.Fatalf("identityLabelValue() produced invalid label value %q: %v", got, err)
	}
}

func TestValidateK8sJobCreateRequestRequiresRunAndNodeIdentity(t *testing.T) {
	err := validateK8sJobCreateRequest(spruntime.JobCreateRequest{
		AttemptRequest: spruntime.AttemptRequest{
			AttemptID: "attempt-1",
			ImageRef:  "tool:latest",
			Command:   []string{"true"},
		},
		JobName:       "job-1",
		Namespace:     "default",
		AttemptMarker: "marker-1",
	})
	if err == nil {
		t.Fatal("validateK8sJobCreateRequest() error = nil, want missing identity rejection")
	}
}

func TestSnapshotIgnoresSameNameJobWithDifferentUID(t *testing.T) {
	client := NewK8sJobClient(fake.NewSimpleClientset(&batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "job-1",
			Namespace: "default",
			UID:       types.UID("new-uid"),
		},
	}))

	snap, err := client.Snapshot(context.Background(), spruntime.BackendRef{
		Namespace: "default",
		Name:      "job-1",
		UID:       "old-uid",
	})
	if err != nil {
		t.Fatalf("Snapshot() error = %v", err)
	}
	if snap.Exists {
		t.Fatal("Snapshot().Exists = true, want false for different UID")
	}
}

func TestDeleteIgnoresSameNameJobWithDifferentUID(t *testing.T) {
	clientset := fake.NewSimpleClientset(&batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "job-1",
			Namespace: "default",
			UID:       types.UID("new-uid"),
		},
	})
	client := NewK8sJobClient(clientset)

	if err := client.Delete(context.Background(), spruntime.BackendRef{
		Namespace: "default",
		Name:      "job-1",
		UID:       "old-uid",
	}); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}

	if _, err := clientset.BatchV1().Jobs("default").Get(context.Background(), "job-1", metav1.GetOptions{}); err != nil {
		t.Fatalf("job was deleted despite UID mismatch: %v", err)
	}
}

func TestPodWatchLabelSelectorIncludesAttemptIdentity(t *testing.T) {
	job := &batchv1.Job{ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{
		labelRunKey:    "run-1",
		labelNodeKey:   "node-1",
		labelAttemptID: "attempt-1",
	}}}

	got := podWatchLabelSelector(job, spruntime.BackendRef{Name: "job-1"})
	for _, want := range []string{
		"job-name=job-1",
		labelRunKey + "=run-1",
		labelNodeKey + "=node-1",
		labelAttemptID + "=attempt-1",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("podWatchLabelSelector() = %q, missing %q", got, want)
		}
	}
}

func TestPodIdentityFilterRejectsStaleAttempt(t *testing.T) {
	expected := map[string]string{
		labelRunKey:    "run-1",
		labelNodeKey:   "node-1",
		labelAttemptID: "attempt-2",
	}
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{
		labelRunKey:    "run-1",
		labelNodeKey:   "node-1",
		labelAttemptID: "attempt-1",
	}}}
	if podMatchesIdentityLabels(pod, expected) {
		t.Fatal("podMatchesIdentityLabels() = true, want false for stale attempt")
	}
}

func TestWatchClosesOnSameNameJobWithDifferentUID(t *testing.T) {
	client := NewK8sJobClient(fake.NewSimpleClientset(&batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "job-1",
			Namespace: "default",
			UID:       types.UID("new-uid"),
		},
	}))

	watch, err := client.Watch(context.Background(), spruntime.BackendRef{
		Namespace: "default",
		Name:      "job-1",
		UID:       "old-uid",
	})
	if err != nil {
		t.Fatalf("Watch() error = %v", err)
	}
	select {
	case _, ok := <-watch.Events:
		if ok {
			t.Fatal("Watch().Events emitted event for different UID")
		}
	case <-time.After(time.Second):
		t.Fatal("Watch().Events did not close for different UID")
	}
}
