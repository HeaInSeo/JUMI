# JUMI Artifact Handoff And Materialization Contract

Status: Canonical

This document defines the Patch 2 contract between JUMI, artifact-handoff, the
Spawner backend, and the node runtime for parent-output to child-input
materialization.

## Execution Model

For a DAG edge:

```text
A -> B
```

node A produces an artifact and node B consumes it as an input. JUMI resolves
that input before B is submitted to the backend. The artifact-handoff service
returns:

```text
resolutionStatus
decision
placementIntent
materializationPlan
materializationCandidates
```

JUMI treats this response as an execution contract. A `RESOLVED` response must
still be executable by the Spawner backend and node runtime. Invalid
placement/materialization combinations fail fast with:

```text
input_materialization_contract_invalid
```

## Placement Intent

Supported placement intent modes are:

```text
preferred_node
required_node
```

`preferred_node` means artifact locality is a performance hint. JUMI maps this
to backend preferred placement, which becomes soft Kubernetes node affinity.
The scheduler may still place the Pod elsewhere.

`required_node` means the artifact cannot be consumed safely elsewhere. JUMI
maps this to backend hard placement, which becomes:

```text
nodeSelector["kubernetes.io/hostname"]
```

Both modes require `placementIntent.nodeName`. A placement mode without a node
name is not executable and must fail fast.

## Materialization Plan

Supported materialization plan modes are:

```text
none
local_reuse
remote_fetch
```

`none` means the resolved input does not need runtime materialization.

`local_reuse` means the child can reuse a node-local source. It requires:

```text
materializationPlan.sourceLocation.nodeLocal.path
```

The source node should be represented either by placement intent or by the
node-local source location. The node runtime must copy materialized input into
the child input area rather than exposing mutable producer/CAS source paths
directly.

`remote_fetch` means the child runtime must fetch or materialize the artifact
from a fetchable source. It requires one of:

```text
materializationPlan.uri
materializationPlan.sourceLocation.http.uri
```

JUMI passes the plan to the node runtime through `JUMI_INPUT_*` environment
variables. HTTP headers or credentials must not be embedded in those env vars.

## Local Path Policy

If artifact-handoff provides `materializationPlan.localPath`, JUMI only accepts
paths under:

```text
inputs/
```

Absolute paths, path traversal, and the `inputs` root itself are rejected before
backend submission. This keeps the materialized child input path inside the
runtime-owned input area.

## Preferred Locality Plus Materialization

The recommended default is:

```text
placementIntent.mode = preferred_node
materializationPlan.mode = remote_fetch or local_reuse
```

This lets the Kubernetes scheduler place B on A's producer node when possible,
while preserving a materialization fallback when the Pod lands elsewhere.

## Required Locality

`required_node` should be used only when remote materialization is unavailable
or unsafe. It can reduce data movement, but it also reduces scheduling freedom.
If the required node has no capacity or is unavailable, the child node may stay
pending or fail at the scheduler/backend layer.

## Post-Scheduling Resolve

When JUMI observes the actual scheduled Pod node, it may resolve the binding
again with:

```text
targetNodeName = <actual pod node>
```

This allows artifact-handoff to choose an execution-specific materialization
candidate after Kubernetes placement is known. The initial resolve still must be
valid enough to submit the child attempt safely.

In Patch 2B this post-scheduling resolve is observation-only. It records the
actual-node decision for telemetry and future materializer integration, but it
does not mutate the already-submitted Pod environment, JobSpec, or runtime
materialization plan. JUMI emits:

```text
node.input_post_scheduling_resolved
observationOnly=true
runtimeEnvUpdated=false
```

If a later materializer or init workload needs to consume the post-scheduling
decision, that workload must fetch a fresh plan after the Pod node is known.
The executor must not pretend that a post-scheduling resolve has changed the
environment variables already rendered into the child attempt.

## Environment Contract

For each resolved input, JUMI injects environment variables with this shape:

```text
JUMI_INPUT_<NAME>_STATUS
JUMI_INPUT_<NAME>_DECISION
JUMI_INPUT_<NAME>_URI
JUMI_INPUT_<NAME>_SOURCE_NODE
JUMI_INPUT_<NAME>_PLACEMENT_MODE
JUMI_INPUT_<NAME>_MATERIALIZATION_MODE
JUMI_INPUT_<NAME>_EXPECTED_DIGEST
JUMI_INPUT_<NAME>_EXPECTED_SIZE_BYTES
JUMI_INPUT_<NAME>_NODE_LOCAL_PATH
JUMI_INPUT_<NAME>_LOCAL_PATH
JUMI_INPUT_<NAME>_REQUIRES_MATERIALIZATION
```

Input names must sanitize to unique environment key segments. Collisions fail
before backend submission.

The runtime materializer contract for supported modes is:

```text
none
→ JUMI_INPUT_<NAME>_REQUIRES_MATERIALIZATION=false

remote_fetch
→ JUMI_INPUT_<NAME>_MATERIALIZATION_MODE=remote_fetch
→ JUMI_INPUT_<NAME>_URI is required
→ JUMI_INPUT_<NAME>_EXPECTED_DIGEST is strongly expected when AH provides it
→ JUMI_INPUT_<NAME>_EXPECTED_SIZE_BYTES is optional
→ JUMI_INPUT_<NAME>_LOCAL_PATH, when present, must be under inputs/

local_reuse
→ JUMI_INPUT_<NAME>_MATERIALIZATION_MODE=local_reuse
→ JUMI_INPUT_<NAME>_NODE_LOCAL_PATH is required
→ JUMI_INPUT_<NAME>_EXPECTED_DIGEST is strongly expected when AH provides it
→ JUMI_INPUT_<NAME>_EXPECTED_SIZE_BYTES is optional
→ JUMI_INPUT_<NAME>_LOCAL_PATH, when present, must be under inputs/
```

The backend adapter also converts these `JUMI_INPUT_*` variables into the node
contract JSON consumed by the node runtime. The suffix set is intentionally
small and guarded:

```text
_URI
_EXPECTED_DIGEST
_EXPECTED_SIZE_BYTES
_MATERIALIZATION_MODE
_NODE_LOCAL_PATH
_LOCAL_PATH
```

Status, decision, placement, source node, and requires-materialization env vars
are kept in the Pod environment for observability and compatibility. Runtime
materialization execution should use the node contract JSON when available.

## Materialization Failure Reasons

Runtime materialization failures must be reported with stable reason strings so
JUMI can surface and eventually map them to retry policy consistently.

The reserved materialization failure reasons are:

```text
input_materialization_digest_mismatch
input_materialization_remote_unavailable
input_materialization_path_rejected
input_materialization_local_source_missing
```

`input_materialization_digest_mismatch` means the materialized bytes did not
match the expected digest. This is a data integrity failure and should not be
silently retried as a generic infrastructure error.

`input_materialization_remote_unavailable` means a `remote_fetch` source could
not be reached or completed. This may be retryable depending on runtime policy
and the underlying error class.

`input_materialization_path_rejected` means the requested local path or source
path violated the runtime path policy.

`input_materialization_local_source_missing` means a `local_reuse` source path
was expected on the scheduled node but was not present or readable.

Patch 2E propagates these reason strings from backend execution results into
attempt, node, and run terminal state. Runtime materializers must return one of
these strings through `terminalFailureReason` when the failure is caused by
input materialization rather than generic backend execution.

## Acceptance Criteria

Patch 2 is complete when:

```text
preferred_node and required_node are the only accepted placement intent modes.
placement modes require nodeName.
none, local_reuse, and remote_fetch are the only accepted materialization modes.
local_reuse requires a node-local source path.
remote_fetch requires a fetchable URI or HTTP source URI.
unsafe materialization localPath values fail before backend submission.
preferred locality remains a soft scheduling hint.
required locality maps to hard nodeSelector placement.
post-scheduling resolve can refine materialization after Pod node observation.
post-scheduling resolve is observation-only until a runtime materializer consumes it.
runtime materializer env suffixes are documented and guarded by tests.
materialization failure reasons are documented and guarded.
runtime materialization failure reasons propagate to attempt, node, and run terminal state.
```
