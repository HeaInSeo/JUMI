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

Notes:
- `GOLANGCI_LINT=/path/to/golangci-lint make lint` can be used to reuse an existing local binary instead of bootstrapping `./bin/golangci-lint`.
- `make lint-security` is intentionally observation-only; current findings should be triaged separately from the fail gate.

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
- [JUMI Durable Registry Options](docs/JUMI_DURABLE_REGISTRY_OPTIONS.ko.md)
- [JUMI Implementation Status 2026-05-14](docs/JUMI_IMPLEMENTATION_STATUS_2026-05-14.ko.md)
- [JUMI Layered Error Guidelines Review](docs/JUMI_LAYERED_ERROR_GUIDELINES_REVIEW.ko.md)
- [JUMI dag-go Integration Contract Review](docs/JUMI_DAG_GO_INTEGRATION_CONTRACT_REVIEW.ko.md)
- [JUMI spawner Integration Contract Review](docs/JUMI_SPAWNER_INTEGRATION_CONTRACT_REVIEW.ko.md)
- [JUMI Locality Semantics Review](docs/JUMI_LOCALITY_SEMANTICS_REVIEW.ko.md)
