# JUMI Constitution

<!--
  JUMI is a DAG execution engine that runs pipeline nodes as Kubernetes Jobs.

  ②-form (D-12), authority revision AR-2026-08-17.1: this file does NOT own
  cross-repo invariants. It consumes the task Authority Snapshot and indexes
  only THIS repo's own enforced constraints. The SoT for those local constraints
  is the rules themselves (`.semgrep/rules/`, Makefile gates, tests) — not this
  prose.
-->

## Cross-repo authority — verified revision-pinned repository mirror

Cross-repo platform meaning is selected by the external Authority Router. For
`AR-2026-08-17.1` the scoped authority chain is:

- platform invariants: `Platform Spec Wiki — CURRENT / 1. constitution`
- platform structure / responsibility / call direction:
  `Platform Spec Wiki — CURRENT / 2. architecture`
- repository-portable invariant mirror: `HeaInSeo/NodeVault` —
  `docs/PLATFORM_MASTER_DESIGN.md §4.1–§4.10`
- mirror verification record: `HeaInSeo/NodeVault` —
  `docs/AUTHORITY_MIRROR_VERIFICATION.md`

JUMI does **not** treat NodeVault §4 as an independent platform canonical. A task
may consume the repository mirror for cross-repo invariant meaning only when
**all** of the following are true:

1. the task `Authority Snapshot` declares `AR-2026-08-17.1`;
2. the NodeVault verification record says `SYNC STATUS: VERIFIED`;
3. the mirror blob SHA matches the blob SHA recorded by that verification record;
4. every scoped/domain/component authority required by the JUMI task is
   explicitly present in the task `Authority Snapshot`;
5. no semantic conflict with the current Authority Router/upstream authority has
   been detected.

If any condition is missing, `STALE`, `UNKNOWN`, mismatched, or conflicting, stop
with `AUTHORITY_CONFLICT`; do not choose a source by timestamp, filename, or
search rank. **Revision equality alone is not sufficient.**

The current repository verification record covers platform invariants only. JUMI
work that depends on platform structure/ownership/call-direction or a specific
execution/materialization/retry contract must carry the exact CURRENT
architecture and relevant scoped/component contract directly in the task
`Authority Snapshot`.

## Process discipline (repo-operational — owned by this repo)

These are operating rules for how change lands in JUMI, not cross-repo
invariants.

- **Deterministic gates are the guarantee.** Merge is decided by deterministic
  checks (tests, Semgrep, gosec, govulncheck, coverage, license, proto-sync).
  LLM/agent review (Code Critic, Requirement Critic, review bots) is
  **advisory**: a passing LLM review never merges on its own, a failing gate is
  never overridden by one. Enforced by `.github/workflows/test.yml` + branch
  ruleset required checks.
- **Spec-anchored change.** Behavioral/contract changes update the relevant
  spec in the same PR (same-commit rule); new feature domains get a
  `specs/{domain}/spec.md` via `/speckit-specify` before implementation.
- **Test-first.** Behavioral changes ship with tests that fail before and pass
  after; suite runs `-shuffle=on -count=1 -race`; coverage stays at/above the
  threshold; regression tests for known defects are permanent.
- **Builder / Critic separation.** The author of a change is not its sole
  judge; a read-only Critic pass reviews before merge, in addition to the gates.
- **Local verify (run before opening a PR):**
  `make lint lint-security-check vuln-check semgrep test test-regression coverage-check license-check handoff-proto-sync-check`.
- **Branch protection.** `main` lands via PR with required status checks and
  resolved review threads; no direct pushes.

## Repo-local enforced constraints (derived index — NOT canonical)

> This section is a derived index of constraints enforced by THIS repo's own
> gates. It is **not** canonical: the SoT is the rule itself (cited per line).
> These are local because no other repo's code can violate them — they concern
> JUMI's own Kubernetes-orchestration internals.

- **SCHED-001** (IMPLEMENTED — `.semgrep/rules/…no-direct-podspec-nodename`):
  never set `PodSpec.NodeName` directly; use `RequiredNodeName` → hostname
  `nodeSelector`. Rationale doc: `docs/JUMI_SCHEDULER_BOUNDARY.ko.md`.
- **K8S-001** (IMPLEMENTED — `.semgrep/rules/…no-job-name-only-pod-watch`):
  Pod watches filter on `jumi.io/run-key`+`node-key`+`attempt-id`, not job-name
  alone. Contract doc: `docs/JUMI_K8S_JOB_LABEL_CONTRACT.md`.
- **K8S-002** (IMPLEMENTED — `.semgrep/rules/…no-job-delete-without-uid-preconditions`):
  Job delete-by-name uses UID `DeleteOptions.Preconditions`.
- **DATA-001** (IMPLEMENTED — `.semgrep/rules/…no-adhoc-materialization-env-key`):
  `JUMI_INPUT_*` env keys only via the `materializationEnvKey` helper.
- **EXEC-001** (IMPLEMENTED — `.semgrep/rules/…no-failed-execution-result-without-reason`):
  a failed `backend.ExecutionResult` must set `TerminalFailureReason`.
- **EXEC-002** (IMPLEMENTED — `.semgrep/rules/…waitnode-must-check-succeeded`):
  `waitAndFinalize` branches on `!result.Succeeded` even on nil error.

Security/supply-chain local gates (SoT = the make targets / CI): `gosec`
(`make lint-security-check`), `govulncheck` (`make vuln-check`), license
(`make license-check`), coverage ≥70% (`make coverage-check`), Kubernetes
manifest lint (`make kube-linter`), CodeQL. All blocking in CI.

## §1.10 — "do not record what you did not observe"

**Authority: CURRENT platform invariant under `AR-2026-08-17.1`. Enforcement in
JUMI: PROPOSED where no deterministic local gate exists.** JUMI currently has no
deterministic rule that generally enforces this invariant.
A live example of the gap: JUMI emits a hardcoded `cleanup_backlog_objects`
constant of `0` without observing actual backlog (`SetCleanupBacklogObjects(0)`,
addressed by E-22). The invariant's authority status and JUMI's enforcement
status are separate axes; do not downgrade the platform invariant because this
repo lacks a gate.

<!--
  Removed from a prior draft (2026-08-02, D-12 ②-form): RETRY-001
  ("RetryPolicy.MaxAttempts is a total-attempt cap"). That is a CROSS-REPO
  invariant — it is the ExecutableRunSpec contract, and spec producers
  (Lowering / PipelineStore) can violate it, not just JUMI. It therefore
  belongs to the platform contract layer, not to this repo constitution.
-->

## Governance

This constitution is versioned and amendable. Changes to the process discipline
or the local-rules index land by PR with a rationale. **Amendment procedure:**
(1) state the rationale; (2) when a local rule's status changes, update the
enforcing gate in the same change; (3) bump the version below — **major** =
a principle/rule removed or redefined, or the source of authority changed;
**minor** = rule added; **patch** = clarification. A rule is `IMPLEMENTED` only
if a deterministic gate enforces it; otherwise `PROPOSED`.

Cross-repo semantics cannot be amended by editing this constitution, a
repository mirror, or its verification record alone. They follow the task's
current Authority Snapshot; a new platform authority revision must be accepted
before repository mirrors are synchronized and independently re-verified.

**Version**: 3.1.0 | **Ratified**: 2026-08-02 | **Last Amended**: 2026-08-17
