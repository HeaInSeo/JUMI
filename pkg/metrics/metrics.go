package metrics

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"

	promclient "github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	promexporter "go.opentelemetry.io/otel/exporters/prometheus"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
)

// Metrics holds all JUMI instrumentation instruments.
// All components share a single instance so metrics appear on one /metrics endpoint.
type Metrics struct {
	// engine
	jobsCreated           metric.Int64Counter
	fastFailTrigger       metric.Int64Counter
	artifactsRegistered   metric.Int64Counter
	inputResolveRequests  metric.Int64Counter
	inputRemoteFetch      metric.Int64Counter
	inputMaterializations metric.Int64Counter
	localityPreferred     metric.Int64Counter
	localityMatched       metric.Int64Counter
	localityMiss          metric.Int64Counter
	localityFallbackStart metric.Int64Counter
	localityFallbackOK    metric.Int64Counter
	localityFallbackFail  metric.Int64Counter
	sampleRunsFinalized   metric.Int64Counter
	gcEvaluateRequests    metric.Int64Counter
	cleanupBacklogObjects metric.Float64Gauge
	// handoff gRPC client
	handoffResolve                metric.Int64Counter
	handoffResolveErrors          metric.Int64Counter
	handoffRegisterArtifact       metric.Int64Counter
	handoffRegisterArtifactErrors metric.Int64Counter
	handoffNotifyTerminal         metric.Int64Counter
	handoffFinalize               metric.Int64Counter
	handoffGCEvaluate             metric.Int64Counter
	// K8s spawner
	k8sNodePrepare       metric.Int64Counter
	k8sNodePrepareErrors metric.Int64Counter
	k8sNodeStart         metric.Int64Counter
	k8sNodeStartErrors   metric.Int64Counter
	k8sNodeSucceeded     metric.Int64Counter
	k8sNodeFailed        metric.Int64Counter
	k8sNodeCanceled      metric.Int64Counter
	k8sNodeCancel        metric.Int64Counter

	handler http.Handler
}

// New creates a Metrics instance backed by an OTel SDK MeterProvider
// that exports to an isolated Prometheus registry.
func New() (*Metrics, error) {
	reg := promclient.NewRegistry()
	exporter, err := promexporter.New(promexporter.WithRegisterer(reg))
	if err != nil {
		return nil, err
	}
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(exporter))
	meter := provider.Meter("github.com/HeaInSeo/JUMI")

	m := &Metrics{handler: promhttp.HandlerFor(reg, promhttp.HandlerOpts{})}

	counters := []struct {
		dest *metric.Int64Counter
		name string
	}{
		{&m.jobsCreated, "jumi_jobs_created"},
		{&m.fastFailTrigger, "jumi_fast_fail_trigger"},
		{&m.artifactsRegistered, "jumi_artifacts_registered"},
		{&m.inputResolveRequests, "jumi_input_resolve_requests"},
		{&m.inputRemoteFetch, "jumi_input_remote_fetch"},
		{&m.inputMaterializations, "jumi_input_materializations"},
		{&m.localityPreferred, "jumi_locality_preferred"},
		{&m.localityMatched, "jumi_locality_matched"},
		{&m.localityMiss, "jumi_locality_miss"},
		{&m.localityFallbackStart, "jumi_locality_fallback_started"},
		{&m.localityFallbackOK, "jumi_locality_fallback_succeeded"},
		{&m.localityFallbackFail, "jumi_locality_fallback_failed"},
		{&m.sampleRunsFinalized, "jumi_sample_runs_finalized"},
		{&m.gcEvaluateRequests, "jumi_gc_evaluate_requests"},
		{&m.handoffResolve, "jumi_handoff_resolve"},
		{&m.handoffResolveErrors, "jumi_handoff_resolve_errors"},
		{&m.handoffRegisterArtifact, "jumi_handoff_register_artifact"},
		{&m.handoffRegisterArtifactErrors, "jumi_handoff_register_artifact_errors"},
		{&m.handoffNotifyTerminal, "jumi_handoff_notify_terminal"},
		{&m.handoffFinalize, "jumi_handoff_finalize"},
		{&m.handoffGCEvaluate, "jumi_handoff_gc_evaluate"},
		{&m.k8sNodePrepare, "jumi_k8s_node_prepare"},
		{&m.k8sNodePrepareErrors, "jumi_k8s_node_prepare_errors"},
		{&m.k8sNodeStart, "jumi_k8s_node_start"},
		{&m.k8sNodeStartErrors, "jumi_k8s_node_start_errors"},
		{&m.k8sNodeSucceeded, "jumi_k8s_node_succeeded"},
		{&m.k8sNodeFailed, "jumi_k8s_node_failed"},
		{&m.k8sNodeCanceled, "jumi_k8s_node_canceled"},
		{&m.k8sNodeCancel, "jumi_k8s_node_cancel"},
	}
	for _, c := range counters {
		if *c.dest, err = meter.Int64Counter(c.name); err != nil {
			return nil, err
		}
	}
	if m.cleanupBacklogObjects, err = meter.Float64Gauge("jumi_cleanup_backlog_objects"); err != nil {
		return nil, err
	}
	return m, nil
}

// Engine metrics
func (m *Metrics) IncJobsCreated()           { m.jobsCreated.Add(context.Background(), 1) }
func (m *Metrics) IncFastFailTrigger()       { m.fastFailTrigger.Add(context.Background(), 1) }
func (m *Metrics) IncArtifactsRegistered()   { m.artifactsRegistered.Add(context.Background(), 1) }
func (m *Metrics) IncInputResolveRequests()  { m.inputResolveRequests.Add(context.Background(), 1) }
func (m *Metrics) IncInputRemoteFetch()      { m.inputRemoteFetch.Add(context.Background(), 1) }
func (m *Metrics) IncInputMaterializations() { m.inputMaterializations.Add(context.Background(), 1) }
func (m *Metrics) IncLocalityPreferred()     { m.localityPreferred.Add(context.Background(), 1) }
func (m *Metrics) IncLocalityMatched()       { m.localityMatched.Add(context.Background(), 1) }
func (m *Metrics) IncLocalityMiss()          { m.localityMiss.Add(context.Background(), 1) }
func (m *Metrics) IncLocalityFallbackStart() { m.localityFallbackStart.Add(context.Background(), 1) }
func (m *Metrics) IncLocalityFallbackOK()    { m.localityFallbackOK.Add(context.Background(), 1) }
func (m *Metrics) IncLocalityFallbackFail()  { m.localityFallbackFail.Add(context.Background(), 1) }
func (m *Metrics) IncSampleRunsFinalized()   { m.sampleRunsFinalized.Add(context.Background(), 1) }
func (m *Metrics) IncGCEvaluateRequests()    { m.gcEvaluateRequests.Add(context.Background(), 1) }
func (m *Metrics) SetCleanupBacklogObjects(v float64) {
	m.cleanupBacklogObjects.Record(context.Background(), v)
}

// Handoff gRPC client metrics
func (m *Metrics) IncHandoffResolve()       { m.handoffResolve.Add(context.Background(), 1) }
func (m *Metrics) IncHandoffResolveErrors() { m.handoffResolveErrors.Add(context.Background(), 1) }
func (m *Metrics) IncHandoffRegisterArtifact() {
	m.handoffRegisterArtifact.Add(context.Background(), 1)
}
func (m *Metrics) IncHandoffRegisterArtifactErrors() {
	m.handoffRegisterArtifactErrors.Add(context.Background(), 1)
}
func (m *Metrics) IncHandoffNotifyTerminal() { m.handoffNotifyTerminal.Add(context.Background(), 1) }
func (m *Metrics) IncHandoffFinalize()       { m.handoffFinalize.Add(context.Background(), 1) }
func (m *Metrics) IncHandoffGCEvaluate()     { m.handoffGCEvaluate.Add(context.Background(), 1) }

// K8s spawner metrics
func (m *Metrics) IncK8sNodePrepare()       { m.k8sNodePrepare.Add(context.Background(), 1) }
func (m *Metrics) IncK8sNodePrepareErrors() { m.k8sNodePrepareErrors.Add(context.Background(), 1) }
func (m *Metrics) IncK8sNodeStart()         { m.k8sNodeStart.Add(context.Background(), 1) }
func (m *Metrics) IncK8sNodeStartErrors()   { m.k8sNodeStartErrors.Add(context.Background(), 1) }
func (m *Metrics) IncK8sNodeSucceeded()     { m.k8sNodeSucceeded.Add(context.Background(), 1) }
func (m *Metrics) IncK8sNodeFailed()        { m.k8sNodeFailed.Add(context.Background(), 1) }
func (m *Metrics) IncK8sNodeCanceled()      { m.k8sNodeCanceled.Add(context.Background(), 1) }
func (m *Metrics) IncK8sNodeCancel()        { m.k8sNodeCancel.Add(context.Background(), 1) }

func (m *Metrics) Handler() http.Handler { return m.handler }

func (m *Metrics) Render() string {
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()
	m.handler.ServeHTTP(rec, req)
	lines := strings.Split(rec.Body.String(), "\n")
	for i, line := range lines {
		if strings.HasPrefix(line, "#") {
			continue
		}
		start := strings.IndexByte(line, '{')
		end := strings.IndexByte(line, '}')
		if start >= 0 && end > start {
			lines[i] = line[:start] + line[end+1:]
		}
	}
	return strings.Join(lines, "\n")
}
