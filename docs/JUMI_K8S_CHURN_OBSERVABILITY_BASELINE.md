# JUMI Kubernetes Churn Observability Baseline

Date: 2026-06-02

## Purpose

This baseline makes Kubernetes churn visible before JUMI, AH, nan, and the later
bori operator track are expanded for larger genomic workloads.

It is not a production-scale GC solution. It is the minimum observable baseline
for deciding whether larger tests are creating object, API, or filesystem debt.

## Scope

The baseline covers one controlled smoke window:

- before the executable DAG smoke starts
- after the smoke finishes
- one namespace, currently `jumi-ah-dev`
- one JUMI run ID

## kube-slint Input

The live smoke script writes:

```text
JUMI/artifacts/devspace/kube-slint-jumi-ah-smoke-metrics.live.json
```

The fixture includes:

- `startMetrics`
- `endMetrics`
- `ahLifecycle`
- `ahArtifacts`
- `k8sChurn.start`
- `k8sChurn.end`

The static replay fixture is:

```text
JUMI/deploy/devspace/fixtures/kube-slint-jumi-ah-smoke-metrics.json
```

## Churn Snapshot Fields

`k8sChurn.start` and `k8sChurn.end` currently record:

- namespace
- runId
- total Jobs in namespace
- Jobs labeled for the run
- active / succeeded / failed Job counts
- total Pods in namespace
- Pods labeled for the run
- Pod phase counts
- pod exec fallback count placeholder

## kube-slint Derived SLI

The `tools/kubeslint-smoke-summary` tool adds derived churn SLI results:

- `k8s_namespace_jobs_total_delta_churn`
- `k8s_namespace_pods_total_delta_churn`
- `k8s_jobs_for_run_churn`
- `k8s_pods_for_run_churn`
- `k8s_failed_jobs_end_churn`
- `k8s_active_jobs_end_churn`

The first two are observation-only deltas. The run-labeled Job/Pod checks make
sure the smoke produced observable K8s objects. Failed/active end checks are
minimum debt signals.

## Command

Replay the static baseline:

```bash
cd /opt/go/src/github.com/HeaInSeo/JUMI
PROFILE=minimum ./scripts/generate-kubeslint-jumi-ah-summary.sh
```

Run live smoke summary generation:

```bash
cd /opt/go/src/github.com/HeaInSeo/JUMI
./scripts/run-jumi-ah-dev-live-smoke-eval.sh
```

## Expansion Points

The next bori/operator track should add:

- namespace ResourceQuota and LimitRange observations
- API server request rate / throttling observations
- etcd object pressure observations where available
- node-local artifact/cache disk usage
- orphan Job/Pod reconciliation status
- AH deletion/tombstone lifecycle counts
- cleanup retry and audit events

## Interpretation

Passing this baseline means:

- the smoke run created observable K8s objects
- no failed or active Job debt was visible at the end of the smoke window
- AH/JUMI counters and lifecycle signals were summarized by kube-slint

It does not mean:

- production-scale genomic workload churn is solved
- node-local cache cleanup is safe
- Kubernetes TTL is sufficient GC
- bori/operator reconciliation is unnecessary
