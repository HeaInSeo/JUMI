package metrics

import (
	"strings"
	"testing"
)

func TestRegistryRender(t *testing.T) {
	reg := NewRegistry()
	reg.IncCounter("jumi_jobs_created_total")
	reg.IncCounter("jumi_artifacts_registered_total")
	reg.IncCounter("jumi_input_resolve_requests_total")
	reg.IncCounter("jumi_sample_runs_finalized_total")
	reg.IncCounter("jumi_gc_evaluate_requests_total")
	reg.SetGauge("jumi_cleanup_backlog_objects", 2)

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
