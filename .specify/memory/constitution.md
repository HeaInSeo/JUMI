# JUMI Constitution

<!--
  JUMI is a DAG execution engine that runs pipeline nodes as Kubernetes Jobs.
  This constitution is deliberately "executable-first": every principle that
  can be, is already enforced by a deterministic gate (Semgrep rule, CI check,
  or test). The prose here EXPLAINS and NAMES those gates and the intent behind
  them — it does not replace them. Where a rule is enforced by tooling, its
  status is IMPLEMENTED and the enforcing gate is named. A rule with no
  deterministic enforcement yet is marked PROPOSED.
-->

## Core Principles

### I. Deterministic Gates Are the Guarantee (NON-NEGOTIABLE)
The decision to merge or release is made by deterministic checks only — tests,
linters, Semgrep rules, coverage, vulnerability and license gates. LLM/agent
reviewers (the Code Critic, the Requirement Critic, and any external review bot)
are **advisory**: they surface findings and raise confidence, but a passing
LLM review is never sufficient to merge, and a failing deterministic gate is
never overridden by an LLM's approval. Status: IMPLEMENTED — enforced by the
blocking CI in `.github/workflows/test.yml` + branch ruleset required checks.

### II. Spec-Anchored Change
Behavioral changes and contract changes are anchored to a spec. When code
changes behavior or a contract, the relevant spec/contract doc is updated in
the **same commit/PR** (the same-commit rule), and code and spec link to each
other (bidirectional links). New feature domains get a `specs/{domain}/spec.md`
authored via `/speckit-specify` before implementation. Existing canonical
contract docs under `docs/` remain the source of truth for their domains.
Status: PARTIAL — spec docs and the spec-kit workflow are IMPLEMENTED; uniform
bidirectional linking is PROPOSED (tracked as adoption follow-up).

### III. Executable Architectural Invariants
The following invariants are enforced by Semgrep AST rules in `.semgrep/rules/`
and must never be violated. They exist because JUMI orchestrates real
Kubernetes side effects where a subtle mistake creates duplicate Jobs, deletes
the wrong Job, or loses failure information.

- **SCHED-001** (IMPLEMENTED — `jumi-no-direct-podspec-nodename`): Never set
  `PodSpec.NodeName` directly. JUMI does not select execution nodes; the
  Kubernetes scheduler decides. Placement is expressed via `RequiredNodeName`
  → hostname `nodeSelector`. Rationale: `docs/JUMI_SCHEDULER_BOUNDARY.ko.md`.
- **K8S-001** (IMPLEMENTED — `jumi-no-job-name-only-pod-watch`): Pod watches
  must filter on `jumi.io/run-key` + `jumi.io/node-key` + `jumi.io/attempt-id`,
  not job-name alone, so stale replacement-attempt Pod events are ignored.
  Contract: `docs/JUMI_K8S_JOB_LABEL_CONTRACT.md`.
- **K8S-002** (IMPLEMENTED — `jumi-no-job-delete-without-uid-preconditions`):
  Job delete-by-name must use UID `DeleteOptions.Preconditions` to avoid
  deleting a same-name replacement Job.
- **DATA-001** (IMPLEMENTED — `jumi-no-adhoc-materialization-env-key`):
  `JUMI_INPUT_*` materialization env keys are produced only via the
  `materializationEnvKey` helper + suffix constants, never hand-written.
  Contract: `docs/JUMI_ARTIFACT_MATERIALIZATION_CONTRACT.md`.
- **EXEC-001** (IMPLEMENTED — `jumi-no-failed-execution-result-without-reason`):
  A failed `backend.ExecutionResult` must set `TerminalFailureReason`.
- **EXEC-002** (IMPLEMENTED — `jumi-waitnode-must-check-succeeded`):
  `waitAndFinalize` must branch on `!result.Succeeded` after `WaitNode` returns
  a nil error — terminal failure can arrive as a result, not a Go error.

### IV. Test-First and Regression Guardrails
Every behavioral change ships with tests that fail before the change and pass
after. The suite runs with `-shuffle=on -count=1` and `-race`. Coverage on core
packages must stay at or above the threshold. Regression tests for known
defects are permanent guardrails, not to be deleted when green. Status:
IMPLEMENTED — `make test`, `make test-regression`, `make coverage-check`
(threshold 70%), all blocking in CI.

Illustrative guardrail — **RETRY-001** (DOCUMENTED → IMPLEMENTED; was a
CONFLICT): `RetryPolicy.MaxAttempts` is a **total-attempt cap**, not a retry
count (`docs/JUMI_EXECUTABLE_RUN_SPEC_DRAFT.ko.md` §9.3: "maxAttempts=1은 실행
1회, 재시도 없음"). The implementation once set `retriesRemaining = MaxAttempts`
(off-by-one) and it went undetected because no test exercised the retry path.
This is exactly the spec↔code drift this constitution exists to prevent.

### V. Contract Stability and Provenance
Cross-repo and cross-component contracts are explicit, versioned, and verified,
not implied. Vendored generated code (e.g. `pkg/handoff/ahv1`) is kept in sync
with its source of truth. Canonical contract docs
(`JUMI_K8S_JOB_LABEL_CONTRACT.md`, `JUMI_ARTIFACT_MATERIALIZATION_CONTRACT.md`,
and the guardrail map/scorecard) must retain their required tokens. Status:
IMPLEMENTED — `make handoff-proto-sync-check`, the cross-repo
`verify-sprint-3d-baseline`, and `hack/quality-guardrails.sh` (the doc↔config
token-contract checker), all blocking in CI.

## Security and Supply-Chain Constraints

- Static analysis (`gosec`) and vulnerability scanning (`govulncheck`) run as
  **blocking** gates: `make lint-security-check`, `make vuln-check`. Report-only
  variants exist for observation but are not the gate.
- Third-party license posture is enforced by `make license-check`.
- SAST via CodeQL and JUMI-specific Semgrep rules run on every PR.
- Kubernetes manifests are linted (`make kube-linter`).
- No secrets or credentials in source, specs, or generated artifacts.
Status: IMPLEMENTED.

## Development Workflow and Quality Gates

- **Builder / Critic separation**: the agent (or human) that writes a change is
  not the sole judge of it. Adversarial review is performed by a separate
  Critic pass (read-only; reports, does not fix) before merge, in addition to
  the deterministic gates.
- **Requirement review left-shift**: non-trivial new features pass an
  adversarial requirement review (problem/assumptions/alternatives) before a
  spec is written, to catch spec-ambiguity defects (the RETRY-001 class) at
  design time rather than at code review.
- **Local verify command** (the deterministic gate, run before opening a PR):
  `make lint lint-security-check vuln-check semgrep test test-regression coverage-check license-check handoff-proto-sync-check`.
- **Branch protection**: `main` is protected by a ruleset; changes land via PR
  with required status checks and resolved review threads. Direct pushes to
  `main` are not permitted.
Status: IMPLEMENTED (gates, branch ruleset); Critic/Requirement-Critic skills
are being adopted via this spec-kit pilot.

## Governance

This constitution supersedes ad-hoc convention. It documents and names the
deterministic enforcement that already exists; it does not create authority that
the gates do not back. Amendments require: (1) a stated rationale, (2) an update
to the enforcing gate when a rule's status changes, and (3) a version bump
below. A principle may only be marked IMPLEMENTED when a deterministic gate
actually enforces it; otherwise it is PROPOSED. LLM/agent review remains
advisory under all amendments (Principle I is non-negotiable).

**Version**: 1.0.0 | **Ratified**: 2026-08-02 | **Last Amended**: 2026-08-02
