package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/HeaInSeo/kube-slint/pkg/slo/engine"
	"github.com/HeaInSeo/kube-slint/pkg/slo/fetch"
	"github.com/HeaInSeo/kube-slint/pkg/slo/summary"
)

type fixture struct {
	RunID        string             `json:"runId"`
	StartedAt    time.Time          `json:"startedAt"`
	FinishedAt   time.Time          `json:"finishedAt"`
	Method       string             `json:"method"`
	Tags         map[string]string  `json:"tags"`
	Evidence     map[string]string  `json:"evidencePaths"`
	StartMetrics map[string]float64 `json:"startMetrics"`
	EndMetrics   map[string]float64 `json:"endMetrics"`
	AHLifecycle  *ahLifecycle       `json:"ahLifecycle,omitempty"`
	AHArtifacts  []ahArtifact       `json:"ahArtifacts,omitempty"`
	K8sChurn     *k8sChurn          `json:"k8sChurn,omitempty"`
}

type k8sChurn struct {
	Namespace string         `json:"namespace,omitempty"`
	Start     k8sChurnSample `json:"start,omitempty"`
	End       k8sChurnSample `json:"end,omitempty"`
}

type k8sChurnSample struct {
	ObservedAt      string `json:"observedAt,omitempty"`
	Namespace       string `json:"namespace,omitempty"`
	RunID           string `json:"runId,omitempty"`
	JobsTotal       int    `json:"jobsTotal"`
	JobsForRun      int    `json:"jobsForRun"`
	JobsActive      int    `json:"jobsActive"`
	JobsSucceeded   int    `json:"jobsSucceeded"`
	JobsFailed      int    `json:"jobsFailed"`
	PodsTotal       int    `json:"podsTotal"`
	PodsForRun      int    `json:"podsForRun"`
	PodsPending     int    `json:"podsPending"`
	PodsRunning     int    `json:"podsRunning"`
	PodsSucceeded   int    `json:"podsSucceeded"`
	PodsFailed      int    `json:"podsFailed"`
	PodsUnknown     int    `json:"podsUnknown"`
	PodExecFallback int    `json:"podExecFallback"`
}

type ahLifecycle struct {
	SampleRunID           string `json:"sampleRunId"`
	Finalized             bool   `json:"finalized"`
	GCEligible            bool   `json:"gcEligible"`
	GCBlockedReason       string `json:"gcBlockedReason"`
	TerminalNodeCount     int    `json:"terminalNodeCount"`
	SucceededNodeCount    int    `json:"succeededNodeCount"`
	RetainedArtifactCount int    `json:"retainedArtifactCount"`
	RetainedArtifactBytes int64  `json:"retainedArtifactBytes"`
}

type ahArtifact struct {
	SampleRunID    string    `json:"sampleRunId"`
	ProducerNodeID string    `json:"producerNodeId"`
	OutputName     string    `json:"outputName"`
	ArtifactID     string    `json:"artifactId"`
	Digest         string    `json:"digest"`
	URI            string    `json:"uri"`
	SizeBytes      int64     `json:"sizeBytes"`
	CreatedAt      time.Time `json:"createdAt"`
}

type staticFetcher struct {
	start fetch.Sample
	end   fetch.Sample
}

func (f *staticFetcher) Fetch(_ context.Context, at time.Time) (fetch.Sample, error) {
	if at.Equal(f.start.At) {
		return f.start, nil
	}
	return f.end, nil
}

func main() {
	inPath := flag.String("in", "", "path to VM-lab metrics fixture JSON")
	outPath := flag.String("out", "", "path to generated sli-summary.json")
	profile := flag.String("profile", "smoke", "spec profile: smoke or minimum")
	normalizeReliability := flag.Bool("normalize-reliability", true, "normalize replay-specific skew values in reliability output")
	flag.Parse()

	if *inPath == "" || *outPath == "" {
		fmt.Fprintln(os.Stderr, "-in and -out are required")
		os.Exit(2)
	}

	fix, err := loadFixture(*inPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "load fixture: %v\n", err)
		os.Exit(1)
	}

	specs, err := pickSpecs(*profile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "pick specs: %v\n", err)
		os.Exit(1)
	}

	fetcher := &staticFetcher{
		start: fetch.Sample{At: fix.StartedAt, Values: fix.StartMetrics},
		end:   fetch.Sample{At: fix.FinishedAt, Values: fix.EndMetrics},
	}
	writer := summary.NewJSONFileWriter()
	eng := engine.New(fetcher, writer, nil)

	method := engine.OutsideSnapshot
	if fix.Method != "" {
		method = engine.MeasurementMethod(fix.Method)
	}

	sum, err := engine.ExecuteStandard(context.Background(), eng, engine.ExecuteRequestStandard{
		Method: method,
		Config: engine.RunConfig{
			RunID:         fix.RunID,
			StartedAt:     fix.StartedAt,
			FinishedAt:    fix.FinishedAt,
			Tags:          fix.Tags,
			Format:        "v4",
			EvidencePaths: fix.Evidence,
		},
		Specs:       specs,
		OutPath:     *outPath,
		Reliability: &summary.Reliability{},
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "execute summary: %v\n", err)
		os.Exit(1)
	}
	if *normalizeReliability {
		normalizeReplayReliability(sum)
	}
	appendLifecycleDerivedResults(sum, fix)
	appendK8sChurnDerivedResults(sum, fix)
	if err := writer.Write(*outPath, *sum); err != nil {
		fmt.Fprintf(os.Stderr, "rewrite normalized summary: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("generated summary: %s\n", *outPath)
	fmt.Printf("results=%d warnings=%d collection=%s evaluation=%s\n",
		len(sum.Results), len(sum.Warnings), sum.Reliability.CollectionStatus, sum.Reliability.EvaluationStatus)
}

func appendK8sChurnDerivedResults(sum *summary.Summary, fix fixture) {
	if sum == nil || fix.K8sChurn == nil {
		return
	}
	churn := fix.K8sChurn
	jobDelta := float64(churn.End.JobsTotal - churn.Start.JobsTotal)
	addResult(sum, summary.SLIResult{
		ID:          "k8s_namespace_jobs_total_delta_churn",
		Title:       "K8s Namespace Jobs Total Delta Churn",
		Unit:        "count",
		Kind:        "derived_delta",
		Description: "Tracks namespace Job object count delta across the smoke measurement window. This is observation only, not production-scale GC assurance.",
		Value:       &jobDelta,
		Status:      summary.StatusPass,
		InputsUsed:  []string{"k8sChurn.start.jobsTotal", "k8sChurn.end.jobsTotal"},
	})

	podDelta := float64(churn.End.PodsTotal - churn.Start.PodsTotal)
	addResult(sum, summary.SLIResult{
		ID:          "k8s_namespace_pods_total_delta_churn",
		Title:       "K8s Namespace Pods Total Delta Churn",
		Unit:        "count",
		Kind:        "derived_delta",
		Description: "Tracks namespace Pod object count delta across the smoke measurement window.",
		Value:       &podDelta,
		Status:      summary.StatusPass,
		InputsUsed:  []string{"k8sChurn.start.podsTotal", "k8sChurn.end.podsTotal"},
	})

	runJobs := float64(churn.End.JobsForRun)
	addResult(sum, summary.SLIResult{
		ID:          "k8s_jobs_for_run_churn",
		Title:       "K8s Jobs For Run Churn",
		Unit:        "count",
		Kind:        "derived_gauge",
		Description: "Tracks Jobs labeled for the smoke run at the end of the window.",
		Value:       &runJobs,
		Status:      statusAtLeast(runJobs, 1),
		InputsUsed:  []string{"k8sChurn.end.jobsForRun"},
	})

	runPods := float64(churn.End.PodsForRun)
	addResult(sum, summary.SLIResult{
		ID:          "k8s_pods_for_run_churn",
		Title:       "K8s Pods For Run Churn",
		Unit:        "count",
		Kind:        "derived_gauge",
		Description: "Tracks Pods labeled for the smoke run at the end of the window.",
		Value:       &runPods,
		Status:      statusAtLeast(runPods, 1),
		InputsUsed:  []string{"k8sChurn.end.podsForRun"},
	})

	failedJobs := float64(churn.End.JobsFailed)
	addResult(sum, summary.SLIResult{
		ID:          "k8s_failed_jobs_end_churn",
		Title:       "K8s Failed Jobs End Churn",
		Unit:        "count",
		Kind:        "derived_gauge",
		Description: "Tracks failed Jobs still visible in the namespace at the end of the window. Non-zero is a churn signal to inspect.",
		Value:       &failedJobs,
		Status:      statusZero(failedJobs),
		InputsUsed:  []string{"k8sChurn.end.jobsFailed"},
	})

	activeJobs := float64(churn.End.JobsActive)
	addResult(sum, summary.SLIResult{
		ID:          "k8s_active_jobs_end_churn",
		Title:       "K8s Active Jobs End Churn",
		Unit:        "count",
		Kind:        "derived_gauge",
		Description: "Tracks active Jobs still visible in the namespace at the end of the smoke window.",
		Value:       &activeJobs,
		Status:      statusZero(activeJobs),
		InputsUsed:  []string{"k8sChurn.end.jobsActive"},
	})
}

func loadFixture(path string) (fixture, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return fixture{}, err
	}
	var f fixture
	if err := json.Unmarshal(data, &f); err != nil {
		return fixture{}, err
	}
	return f, nil
}

func pickSpecs(profile string) ([]sliSpec, error) {
	switch profile {
	case "smoke":
		return jumiAHSmokeGuardrailSpecs(), nil
	case "minimum":
		return jumiAHMinimumSpecs(), nil
	default:
		return nil, fmt.Errorf("unknown profile %q", profile)
	}
}

func normalizeReplayReliability(sum *summary.Summary) {
	if sum == nil || sum.Reliability == nil {
		return
	}
	sum.Reliability.ConfigSourceType = "injected"
	sum.Reliability.ConfigSourcePath = "fixture_replay"
	sum.Reliability.StartSkewMs = nil
	sum.Reliability.EndSkewMs = nil
	if sum.Reliability.CollectionStatus == "Complete" && sum.Reliability.EvaluationStatus == "Complete" {
		score := 1.0
		sum.Reliability.ConfidenceScore = &score
	}
}

func appendLifecycleDerivedResults(sum *summary.Summary, fix fixture) {
	if sum == nil || fix.AHLifecycle == nil {
		return
	}
	addResult(sum, summary.SLIResult{
		ID:          "ah_lifecycle_finalized_smoke",
		Title:       "AH Lifecycle Finalized Smoke",
		Unit:        "bool",
		Kind:        "derived_state",
		Description: "Smoke run should finalize the AH sample-run lifecycle.",
		Value:       boolValue(fix.AHLifecycle.Finalized),
		Status:      boolStatus(fix.AHLifecycle.Finalized),
		InputsUsed:  []string{"ahLifecycle.finalized"},
	})

	retentionWindowActive := fix.AHLifecycle.Finalized && !fix.AHLifecycle.GCEligible && fix.AHLifecycle.GCBlockedReason == "retention_window_active"
	addResult(sum, summary.SLIResult{
		ID:          "ah_retention_window_active_smoke",
		Title:       "AH Retention Window Active Smoke",
		Unit:        "bool",
		Kind:        "derived_state",
		Description: "Immediately after finalize+GC evaluate, AH should retain the sample-run under the active retention window.",
		Value:       boolValue(retentionWindowActive),
		Status:      boolStatus(retentionWindowActive),
		InputsUsed:  []string{"ahLifecycle.finalized", "ahLifecycle.gcEligible", "ahLifecycle.gcBlockedReason"},
	})

	retainedBytes := float64(fix.AHLifecycle.RetainedArtifactBytes)
	addResult(sum, summary.SLIResult{
		ID:          "ah_retained_artifact_bytes_smoke",
		Title:       "AH Retained Artifact Bytes Smoke",
		Unit:        "bytes",
		Kind:        "derived_gauge",
		Description: "AH lifecycle retainedArtifactBytes should reflect the retained producer outputs for the sample run.",
		Value:       &retainedBytes,
		Status:      statusForPositiveOrZero(retainedBytes, fix.AHLifecycle.RetainedArtifactCount > 0),
		InputsUsed:  []string{"ahLifecycle.retainedArtifactCount", "ahLifecycle.retainedArtifactBytes"},
	})

	completeCount, totalBytes, allComplete := artifactMetadataCompleteness(fix.AHArtifacts)
	completeCountValue := float64(completeCount)
	addResult(sum, summary.SLIResult{
		ID:          "ah_artifact_metadata_complete_smoke",
		Title:       "AH Artifact Metadata Complete Smoke",
		Unit:        "count",
		Kind:        "derived_state",
		Description: "AH inventory should retain digest, uri, and sizeBytes for all smoke-run producer artifacts.",
		Value:       &completeCountValue,
		Status:      completenessStatus(len(fix.AHArtifacts), allComplete),
		InputsUsed:  []string{"ahArtifacts.digest", "ahArtifacts.uri", "ahArtifacts.sizeBytes"},
	})

	lifecycleBytesMatch := len(fix.AHArtifacts) > 0 && totalBytes == fix.AHLifecycle.RetainedArtifactBytes
	addResult(sum, summary.SLIResult{
		ID:          "ah_inventory_lifecycle_bytes_match_smoke",
		Title:       "AH Inventory Lifecycle Bytes Match Smoke",
		Unit:        "bool",
		Kind:        "derived_state",
		Description: "AH lifecycle retainedArtifactBytes should match the sum of retained artifact sizeBytes in inventory.",
		Value:       boolValue(lifecycleBytesMatch),
		Status:      boolStatus(lifecycleBytesMatch),
		InputsUsed:  []string{"ahLifecycle.retainedArtifactBytes", "ahArtifacts.sizeBytes"},
	})
}

func addResult(sum *summary.Summary, result summary.SLIResult) {
	sum.Results = append(sum.Results, result)
}

func boolValue(v bool) *float64 {
	if v {
		x := 1.0
		return &x
	}
	x := 0.0
	return &x
}

func boolStatus(v bool) summary.Status {
	if v {
		return summary.StatusPass
	}
	return summary.StatusFail
}

func statusForPositiveOrZero(value float64, expectPositive bool) summary.Status {
	if expectPositive {
		if value > 0 {
			return summary.StatusPass
		}
		return summary.StatusFail
	}
	if value == 0 {
		return summary.StatusPass
	}
	return summary.StatusFail
}

func statusAtLeast(value float64, min float64) summary.Status {
	if value >= min {
		return summary.StatusPass
	}
	return summary.StatusFail
}

func statusZero(value float64) summary.Status {
	if value == 0 {
		return summary.StatusPass
	}
	return summary.StatusFail
}

func artifactMetadataCompleteness(artifacts []ahArtifact) (completeCount int, totalBytes int64, allComplete bool) {
	allComplete = len(artifacts) > 0
	for _, artifact := range artifacts {
		complete := artifact.Digest != "" && artifact.URI != "" && artifact.SizeBytes >= 0
		if complete {
			completeCount++
			totalBytes += artifact.SizeBytes
			continue
		}
		allComplete = false
	}
	if len(artifacts) == 0 {
		allComplete = false
	}
	return completeCount, totalBytes, allComplete
}

func completenessStatus(total int, allComplete bool) summary.Status {
	if total == 0 || !allComplete {
		return summary.StatusFail
	}
	return summary.StatusPass
}
