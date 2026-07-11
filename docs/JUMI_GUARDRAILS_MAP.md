# JUMI Guardrails Map

This document maps JUMI's current guardrails to the contracts they protect.

## Quality Guardrails

`make quality-guardrails` runs `hack/quality-guardrails.sh`.

It is a source-of-truth check for design contracts that should not silently
drift:

- Job and Pod identity label contract.
- Attempt selector safety.
- Kueue label ownership.
- Spawner golden YAML fixture coverage.
- Artifact materialization env contract.
- Runtime materialization failure reason propagation.
- Bad fixture and smoke acceptance matrix.
- CI, Semgrep, and kube-linter wiring.

The GitHub Actions workflow runs this check when contract files, workflows,
Spawner code, Semgrep rules, or golden fixtures change.

## Semgrep

`make semgrep` runs custom Semgrep rules over `cmd` and `pkg`.
`make semgrep-test` validates each rule against its fixture file.

Current custom rules block:

- Direct `PodSpec.NodeName` binding.
- Pod watches that use only the Kubernetes `job-name` label.
- Job deletes without UID preconditions.
- Ad hoc `JUMI_INPUT_*` materialization env key construction.
- Failed `backend.ExecutionResult` values without `TerminalFailureReason`.
- Removing the `!result.Succeeded` branch from `waitAndFinalize`.

## Golden Fixtures

Spawner Job YAML fixtures live in `pkg/spawner/testdata`.

They cover:

- preferred `remote_fetch` with Kueue and soft node affinity.
- required `local_reuse` with hard hostname nodeSelector.
- plain non-Kueue Job rendering.

If intended Spawner rendering changes, update the fixtures with:

```sh
make update-spawner-fixtures
```

The golden test fails with the same command when rendered YAML drifts.

## Test Strategy

[JUMI_TEST_STRATEGY.md](JUMI_TEST_STRATEGY.md) is the canonical guardrail
planning source for:

- spec validation bad fixtures.
- artifact resolution bad fixtures.
- runtime failure reason propagation fixtures.
- generated Job golden fixture matrix.
- smoke acceptance criteria.

It deliberately separates contract smoke from materialization smoke so an
external runtime failure is not reported as a successful JUMI node.

## Kube-Linter

`make kube-linter` lints static manifests in `deploy/k8s`.

Generated Spawner Job fixtures are intentionally guarded by golden tests and
quality guardrails instead of kube-linter because they are Job render outputs,
not deployable static manifests.

## Sprint Baseline

The sprint baseline keeps JUMI aligned with the external artifact-handoff and
node-artifact-runtime repositories without putting their implementation details
inside this repository.
