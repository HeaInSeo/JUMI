# JUMI Test Strategy

Status: Canonical guardrail planning source

This document defines JUMI's bad fixture matrix and smoke acceptance matrix.
It does not move `nan` or node-artifact-runtime implementation into this
repository. It defines which contracts JUMI must keep guarded while those
runtime pieces evolve separately.

## Bad Fixture Matrix

Bad fixtures are inputs that must fail before Kubernetes Job submission or
before a node is allowed to report success.

### Spec Validation Fixtures

| Case | Expected result | Current guard |
|---|---|---|
| user label uses `jumi.io/*` | reject fail-fast | `TestValidateK8sJobCreateRequestRejectsReservedUserLabel` |
| user label uses `spawner.io/*` | reject fail-fast | spawner label validation tests |
| user label uses `kueue.x-k8s.io/*` directly | reject fail-fast | spec/backend Kueue label tests |
| `requiredNodeName` conflicts with hostname `nodeSelector` | reject fail-fast | `TestValidateK8sJobCreateRequestRejectsRequiredNodeConflict` |
| `RequiredNodeName` bypasses scheduler with PodSpec `nodeName` | reject statically | Semgrep `jumi-no-direct-podspec-nodename` |

### Artifact Resolution Fixtures

| Case | Expected result | Current guard |
|---|---|---|
| placement mode is `required_node` but `nodeName` is empty | `input_materialization_contract_invalid` | `TestValidateResolvedBindingContractRejectsPlacementWithoutNode` |
| placement mode is unsupported | `input_materialization_contract_invalid` | materialization contract validation |
| materialization mode is `local_reuse` without node-local source | `input_materialization_contract_invalid` | `TestValidateResolvedBindingContractRejectsLocalReuseWithoutNodeLocalSource` |
| materialization mode is `remote_fetch` without URI or HTTP source | `input_materialization_contract_invalid` | `TestValidateResolvedBindingContractRejectsRemoteFetchWithoutSource` |
| materialization `localPath` escapes `inputs/` | `input_materialization_contract_invalid` | `TestValidateResolvedBindingContractRejectsUnsafeLocalPath` |
| input env names collide after sanitization | `input_env_key_collision` | `TestValidateResolvedBindingEnvKeysRejectsCollisions` |

### Runtime Failure Fixtures

These are backend terminal results, not `nan` implementation tests. The runtime
may live outside this repository, but JUMI must preserve the reason strings.

| Case | Expected result | Current guard |
|---|---|---|
| materialized bytes fail digest verification | `input_materialization_digest_mismatch` | `TestDagEnginePropagatesRuntimeMaterializationFailureReasons` |
| remote source cannot be fetched | `input_materialization_remote_unavailable` | `TestDagEnginePropagatesRuntimeMaterializationFailureReasons` |
| runtime rejects target/source path | `input_materialization_path_rejected` | `TestDagEnginePropagatesRuntimeMaterializationFailureReasons` |
| expected node-local source is missing | `input_materialization_local_source_missing` | `TestDagEnginePropagatesRuntimeMaterializationFailureReasons` |
| backend returns `Succeeded:false` with no reason | reject statically | Semgrep `jumi-no-failed-execution-result-without-reason` |
| `waitAndFinalize` ignores `Succeeded:false` result | reject statically | Semgrep `jumi-waitnode-must-check-succeeded` |

## Golden Job Fixture Matrix

Spawner golden fixtures are rendered Job YAML outputs, not deploy manifests.
They are guarded by Go golden tests and quality guardrails.

| Fixture | Contract |
|---|---|
| `k8s-job-preferred-remote-fetch-kueue.golden.yaml` | Kueue queue label, `suspend:true`, preferred node affinity, PVC, `remote_fetch` env |
| `k8s-job-required-local-reuse.golden.yaml` | `suspend:false`, hard hostname nodeSelector, `local_reuse`, node-local path env |
| `k8s-job-plain-non-kueue.golden.yaml` | no Kueue label, no materialization env, no affinity |

Update intentionally with:

```sh
make update-spawner-fixtures
```

## Smoke Acceptance Matrix

Smoke tests must distinguish contract smoke, materialization smoke, and policy
or observability smoke. A failure in one layer should not be misreported as a
different layer.

| ID | Scenario | Required assertions |
|---|---|---|
| SMOKE-1 | plain non-Kueue Job submit | Job reaches terminal success; identity labels select the current attempt |
| SMOKE-2 | Kueue `remote_fetch` contract | Job starts suspended; queue label is present only when Kueue is enabled; runtime env contains URI, digest, size, local path |
| SMOKE-3 | required `local_reuse` contract | hostname nodeSelector is present; node-local path env is present; no Kueue label unless explicitly enabled |
| SMOKE-4 | remote materialization failure | terminal run/node/attempt failure reason is `input_materialization_remote_unavailable` or `input_materialization_digest_mismatch` |
| SMOKE-5 | local reuse materialization failure | terminal run/node/attempt failure reason is `input_materialization_local_source_missing` or `input_materialization_path_rejected` |
| SMOKE-6 | stale attempt event | event with mismatched attempt identity is ignored and does not flip current attempt state |
| SMOKE-7 | cleanup by attempt | attempt cleanup does not delete a same-name different-UID replacement Job |
| SMOKE-8 | scheduler boundary | `RequiredNodeName` materializes as hostname nodeSelector, never PodSpec `nodeName` |
| SMOKE-9 | post-scheduling resolve | observed Pod node can trigger observation-only resolve; runtime env is not mutated after backend start |
| SMOKE-10 | churn/observability summary | smoke leaves no active/failed Job debt at the end of the controlled window |

## Acceptance Rules

Do not accept a smoke as passing if:

- a stale attempt event updates current attempt state;
- a failed materialization is reported as generic success;
- `resources.requests`, PVC, nodeSelector, and node affinity are not visible in the rendered Job where expected;
- Kueue queue labels can be injected by user labels;
- generated Job YAML changes without golden fixture review;
- smoke summary merges contract smoke and materialization smoke into one ambiguous result;
- runtime failure reasons are lost before attempt, node, and run terminal state.

## CI Placement

| Layer | Command or workflow | Purpose |
|---|---|---|
| unit/regression | `make test` | Go contract tests and DAG failure propagation |
| generated Job contract | `go test ./pkg/spawner -run TestRenderedK8sJobGoldenFixtures` | golden YAML drift detection |
| static code guardrails | `make semgrep` and `make semgrep-test` | custom rule enforcement and rule fixture validation |
| static manifest lint | `make kube-linter` | deploy manifest linting |
| source-of-truth guardrails | `make quality-guardrails` | docs, CI, Semgrep, golden fixture, materialization contract wiring |
| cross-repo baseline | `make verify-sprint-3d-baseline` | JUMI/AH/node-artifact-runtime contract alignment |
| manual live smoke | `make verify-sprint-3d-remote` | remote_fetch and same-node local_reuse live smoke |
