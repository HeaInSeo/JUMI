package metrics

import (
	"strings"
	"testing"
)

func TestRegistryRender(t *testing.T) {
	reg, err := New()
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	reg.IncJobsCreated()
	reg.IncArtifactsRegistered()
	reg.IncInputResolveRequests()
	reg.IncLocalityPreferred()
	reg.IncLocalityMiss()
	reg.IncLocalityFallbackStart()
	reg.IncSampleRunsFinalized()
	reg.IncGCEvaluateRequests()
	reg.SetCleanupBacklogObjects(2)

	rendered := reg.Render()
	if !strings.Contains(rendered, "jumi_jobs_created_total 1") {
		t.Fatalf("missing counter in render: %s", rendered)
	}
	if !strings.Contains(rendered, "jumi_artifacts_registered_total 1") {
		t.Fatalf("missing artifact counter in render: %s", rendered)
	}
	if !strings.Contains(rendered, "jumi_input_resolve_requests_total 1") {
		t.Fatalf("missing input resolve counter in render: %s", rendered)
	}
	if !strings.Contains(rendered, "jumi_locality_preferred_total 1") {
		t.Fatalf("missing locality preferred counter in render: %s", rendered)
	}
	if !strings.Contains(rendered, "jumi_locality_miss_total 1") {
		t.Fatalf("missing locality miss counter in render: %s", rendered)
	}
	if !strings.Contains(rendered, "jumi_locality_fallback_started_total 1") {
		t.Fatalf("missing locality fallback counter in render: %s", rendered)
	}
	if !strings.Contains(rendered, "jumi_sample_runs_finalized_total 1") {
		t.Fatalf("missing sample-run finalized counter in render: %s", rendered)
	}
	if !strings.Contains(rendered, "jumi_gc_evaluate_requests_total 1") {
		t.Fatalf("missing gc evaluate counter in render: %s", rendered)
	}
	if !strings.Contains(rendered, "jumi_cleanup_backlog_objects 2") {
		t.Fatalf("missing gauge in render: %s", rendered)
	}
}

func TestRegistryRenderOmitsUntouchedMetrics(t *testing.T) {
	reg, err := New()
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	rendered := reg.Render()
	if strings.Contains(rendered, "jumi_jobs_created_total") {
		t.Fatalf("unexpected untouched counter in render: %s", rendered)
	}
	if strings.Contains(rendered, "jumi_cleanup_backlog_objects") {
		t.Fatalf("unexpected untouched gauge in render: %s", rendered)
	}
}
