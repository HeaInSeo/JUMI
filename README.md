# JUMI

Policy-agnostic in-cluster execution data-plane app for Kubernetes, built on a Kubernetes native baseline and integrated with Kueue without making core execution semantics depend on it.

## Commands

- `make test`: run the root-module unit test suite
- `make test-regression`: rerun the AH seam, executor, metrics, and observe regression packages
- `make coverage`: generate `reports/cover.out` and `reports/coverage.txt`
- `make lint`: fail gate for root-module code
- `make lint-security`: report-only `gosec` observation
- `make vuln`: report-only `govulncheck` observation
- `make vuln-all`: report-only `govulncheck` over all packages
- `make preflight-publish-local`: check Harbor reachability from the current host
- `make preflight-publish-remote`: check Harbor reachability from `100.123.80.48`
- `make preflight-ko-remote`: verify remote `ko`, Harbor TLS trust, and Docker auth on `100.123.80.48`
- `make runtime-build-local`: local runtime image build for CLI validation only
- `make runtime-check-local`: local `nan` CLI contract check against the local image
- `make runtime-smoke-remote`: remote K8s smoke entrypoint
- `make ko-publish-remote`: build and deploy the JUMI service image with `ko` on `100.123.80.48`
- `make ko-smoke-remote`: run the live smoke after a `ko` service image publish

Notes:
- `GOLANGCI_LINT=/path/to/golangci-lint make lint` can be used to reuse an existing local binary instead of bootstrapping `./bin/golangci-lint`.
- `make lint-security` is intentionally observation-only; current findings should be triaged separately from the fail gate.
- Local build is not publish authority. Runtime image publish and K8s smoke authority live on `100.123.80.48`.
- Remote `ko` publish requires Harbor TLS trust in the remote OS trust store and Harbor auth in `~/.docker/config.json`.

## Documents

- [JUMI Final Development Goal](docs/JUMI_FINAL_DEVELOPMENT_GOAL.ko.md)
- [JUMI Design](docs/JUMI_DESIGN.ko.md)
- [JUMI State Transition Spec](docs/JUMI_STATE_TRANSITION_SPEC.ko.md)
- [JUMI Cancel / Failure / Retry Semantics](docs/JUMI_CANCEL_FAILURE_RETRY_SEMANTICS.ko.md)
- [JUMI gRPC Contract Draft](docs/JUMI_GRPC_CONTRACT_DRAFT.ko.md)
- [JUMI Executable Run Spec Draft](docs/JUMI_EXECUTABLE_RUN_SPEC_DRAFT.ko.md)
- [JUMI Sprint Plan](docs/JUMI_SPRINT_PLAN.ko.md)
- [JUMI Scheduler Boundary](docs/JUMI_SCHEDULER_BOUNDARY.ko.md)
- [JUMI Node Runtime Artifact Contract](docs/JUMI_NODE_RUNTIME_ARTIFACT_CONTRACT.md)
- [JUMI ko Service Image Migration Plan](docs/JUMI_KO_SERVICE_IMAGE_MIGRATION_PLAN.md)
- [JUMI Node Runtime Base Image Plan](docs/JUMI_NODE_RUNTIME_BASE_IMAGE_PLAN.md)
- [JUMI Artifact Helper Repo Split Plan](docs/JUMI_ARTIFACT_HELPER_REPO_SPLIT_PLAN.md)
- [JUMI AH nan Integration Review](docs/JUMI_AH_NAN_INTEGRATION_REVIEW.md)
- [JUMI Remote Fetch v0 Sprint Plan](docs/JUMI_REMOTE_FETCH_V0_SPRINT_PLAN.md)
- [JUMI simple_http Artifact Source v0 Plan](docs/JUMI_SIMPLE_HTTP_ARTIFACT_SOURCE_V0_PLAN.md)
- [Sprint 3B Design: Pure K8s Node-local Handoff Happy Path](docs/JUMI_SPRINT_3B_PURE_K8S_NODE_LOCAL_HANDOFF.md)
- [Artifact Source Registry / Materialization Source Layer](docs/JUMI_ARTIFACT_SOURCE_REGISTRY_DESIGN.md)
- [JUMI Durable Registry Options](docs/JUMI_DURABLE_REGISTRY_OPTIONS.ko.md)
- [JUMI Implementation Status 2026-05-14](docs/JUMI_IMPLEMENTATION_STATUS_2026-05-14.ko.md)
- [JUMI Layered Error Guidelines Review](docs/JUMI_LAYERED_ERROR_GUIDELINES_REVIEW.ko.md)
- [JUMI dag-go Integration Contract Review](docs/JUMI_DAG_GO_INTEGRATION_CONTRACT_REVIEW.ko.md)
- [JUMI dag-go Migration Checklist](docs/JUMI_DAG_GO_MIGRATION_CHECKLIST.ko.md)
- [JUMI spawner Integration Contract Review](docs/JUMI_SPAWNER_INTEGRATION_CONTRACT_REVIEW.ko.md)
- [JUMI Locality Semantics Review](docs/JUMI_LOCALITY_SEMANTICS_REVIEW.ko.md)
- [JUMI Execution Environment Boundary](docs/JUMI_EXECUTION_ENVIRONMENT_BOUNDARY.md)
