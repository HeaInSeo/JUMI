# Cleanup TTL Eviction Scope Decision

Date: 2026-06-01

## Decision

The remaining cleanup/TTL/eviction item is split by responsibility instead of
being implemented as one JUMI-local controller.

Current JUMI/AH/nan work keeps:

- Kubernetes Job cleanup through `ttlSecondsAfterFinished`.
- AH lifecycle finalization and GC eligibility evaluation.
- lifecycle and GC backlog visibility metrics.

Actual artifact eviction is deferred to the later operator/control-plane track
because it needs operational reconciliation, node-local safety, and fleet-level
observability.

## Current State

JUMI:

- Propagates per-node cleanup policy to Kubernetes Job TTL.
- Calls AH `FinalizeSampleRun`.
- Calls AH `EvaluateGC` after finalization.
- Exposes cleanup backlog visibility through metrics.

AH:

- Persists sample-run lifecycle fields.
- Computes retention windows.
- Marks GC eligibility.
- Estimates retained artifact bytes as GC backlog.

nan:

- Materializes inputs inside a node execution.
- Verifies digest and size.
- Does not own long-lived node-local cache lifecycle.

## Not In JUMI Now

JUMI should not directly delete:

- node-local artifact cache files
- AH persisted artifact metadata
- Kubernetes resources beyond the Job lifecycle it created
- future dataplane app runtime state

Reasons:

- JUMI is a DAG executor, not a fleet reconciler.
- node-local deletion requires per-node safety checks.
- eviction needs policy, retry, backoff, and audit semantics.
- bori is intended to become the operational operator/control-plane layer.

## Later Operator Track

bori should eventually own:

- retention policy reconciliation
- artifact/cache eviction orchestration
- operational gate visibility
- rollout/version-aware cleanup behavior
- fleet-level observability and alerts

AH should provide:

- lifecycle state
- GC eligibility
- artifact/source inventory
- explicit deletion or tombstone APIs when the operator track needs them

nan or a node-local component should provide:

- safe deletion of node-local materialized/cache paths
- node-local disk pressure signals
- per-node cleanup result reporting

## Next Concrete Work

Do not add a JUMI cleanup controller until the bori operator boundary and AH
deletion/tombstone API are designed.

The next safe implementation step in the JUMI track is backend decomposition:
make the direct Kubernetes/nan execution path first-class while preserving
spawner compatibility.
