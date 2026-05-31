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

func TestMetricsHandoff(t *testing.T) {
	reg, err := New()
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	reg.IncHandoffResolve()
	reg.IncHandoffResolveErrors()
	reg.IncHandoffRegisterArtifact()
	reg.IncHandoffRegisterArtifactErrors()
	reg.IncHandoffNotifyTerminal()
	reg.IncHandoffFinalize()
	reg.IncHandoffGCEvaluate()

	rendered := reg.Render()
	for _, want := range []string{
		"jumi_handoff_resolve_total 1",
		"jumi_handoff_resolve_errors_total 1",
		"jumi_handoff_register_artifact_total 1",
		"jumi_handoff_register_artifact_errors_total 1",
		"jumi_handoff_notify_terminal_total 1",
		"jumi_handoff_finalize_total 1",
		"jumi_handoff_gc_evaluate_total 1",
	} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("missing %q in render:\n%s", want, rendered)
		}
	}
}

func TestMetricsK8sSpawner(t *testing.T) {
	reg, err := New()
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	reg.IncK8sNodePrepare()
	reg.IncK8sNodePrepareErrors()
	reg.IncK8sNodeStart()
	reg.IncK8sNodeStartErrors()
	reg.IncK8sNodeSucceeded()
	reg.IncK8sNodeFailed()
	reg.IncK8sNodeCanceled()
	reg.IncK8sNodeCancel()

	rendered := reg.Render()
	for _, want := range []string{
		"jumi_k8s_node_prepare_total 1",
		"jumi_k8s_node_prepare_errors_total 1",
		"jumi_k8s_node_start_total 1",
		"jumi_k8s_node_start_errors_total 1",
		"jumi_k8s_node_succeeded_total 1",
		"jumi_k8s_node_failed_total 1",
		"jumi_k8s_node_canceled_total 1",
		"jumi_k8s_node_cancel_total 1",
	} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("missing %q in render:\n%s", want, rendered)
		}
	}
}

func TestMetricsLocalityAndInput(t *testing.T) {
	reg, err := New()
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	reg.IncFastFailTrigger()
	reg.IncInputRemoteFetch()
	reg.IncInputMaterializations()
	reg.IncLocalityMatched()
	reg.IncLocalityFallbackOK()
	reg.IncLocalityFallbackFail()

	rendered := reg.Render()
	for _, want := range []string{
		"jumi_fast_fail_trigger_total 1",
		"jumi_input_remote_fetch_total 1",
		"jumi_input_materializations_total 1",
		"jumi_locality_matched_total 1",
		"jumi_locality_fallback_succeeded_total 1",
		"jumi_locality_fallback_failed_total 1",
	} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("missing %q in render:\n%s", want, rendered)
		}
	}
}

func TestMetricsHandlerNotNil(t *testing.T) {
	reg, err := New()
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if reg.Handler() == nil {
		t.Fatal("Handler() returned nil")
	}
}
