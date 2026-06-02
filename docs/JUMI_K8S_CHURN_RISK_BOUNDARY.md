# JUMI Kubernetes Churn Risk Boundary

Date: 2026-06-02

## Conclusion

JUMI can run executable DAG fixtures through Kubernetes Jobs, AH, and nan, but
it is not yet a production-scale genomic workload churn controller.

The current design creates Kubernetes Jobs dynamically through client-go and
uses a stateful in-memory execution registry with fast-fail semantics. That is
acceptable for controlled smoke tests and small bounded runs, but it does not
fully address high-volume genomic analysis churn.

## Current Mitigations

JUMI already has some limited guardrails:

- bounded release concurrency through `JUMI_MAX_CONCURRENT_RELEASE`
- per-node Kubernetes Job TTL through `ttlSecondsAfterFinished`
- fast-fail propagation to avoid continuing known-failed DAG branches
- AH lifecycle finalization and GC eligibility visibility
- output manifest collection and artifact registration after node completion

These are not enough to claim production readiness for unbounded workloads.

## Churn Risks Not Closed

Kubernetes object churn:

- many Jobs and Pods can be created over a short period
- fast-fail workloads can still create many short-lived failed objects
- TTL cleanup is asynchronous and can lag under load
- etcd object count and watch pressure are not monitored by JUMI

client-go/API server pressure:

- JUMI uses client-go for create/get/list/delete/exec-style operations
- API QPS/burst policy is not yet modeled as a first-class operational setting
- polling/wait behavior can amplify API pressure under large runs

state recovery:

- the current default registry is in-memory
- if JUMI restarts, it can lose run state while Jobs/Pods remain in Kubernetes
- orphan reconciliation is not implemented

node-local filesystem churn:

- local artifact/cache directories can accumulate
- JUMI does not perform safe node-local deletion
- disk pressure, cleanup retry, and deletion audit semantics are not implemented

artifact/metadata churn:

- AH can mark lifecycle and GC eligibility
- actual deletion/tombstone flow is not implemented in this JUMI track
- retained artifact bytes are visibility, not enforcement

## Production Blocker Statement

Before running large-scale genomic analysis workloads, the system needs an
operational churn controller or operator layer that can reconcile:

- run state
- Kubernetes Jobs/Pods
- AH lifecycle and artifact inventory
- node-local cache paths
- retention and deletion policy
- metrics, alerts, and audit logs

This belongs to the bori/operator direction rather than being hidden inside
JUMI's DAG executor.

## Minimum Guardrails Before Larger Tests

For larger but still controlled tests, require:

- a bounded namespace with ResourceQuota and LimitRange
- explicit `JUMI_MAX_CONCURRENT_RELEASE`
- short but safe Job TTL values
- AH retention window configured for the test profile
- dashboard/metrics for Job count, Pod count, failed Job backlog, and disk usage
- manual cleanup runbook for orphan Jobs and node-local paths
- kube-slint churn summary based on
  [JUMI_K8S_CHURN_OBSERVABILITY_BASELINE.md](./JUMI_K8S_CHURN_OBSERVABILITY_BASELINE.md)

## Direction

JUMI should remain the executable DAG dataplane app.

bori should become the operational control-plane/operator that owns churn
reconciliation, rollout-aware cleanup, observability, and policy enforcement.

## Non-Goals For Current JUMI Track

- hiding unbounded production churn behind JUMI fast-fail semantics
- direct node-local cache deletion from JUMI
- treating Kubernetes TTL as sufficient GC
- claiming authored pipeline end-to-end production readiness from executable
  fixture smoke tests
