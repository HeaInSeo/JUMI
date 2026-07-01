package spawner

import (
	"testing"

	spruntime "github.com/HeaInSeo/spawner/pkg/runtime"
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
