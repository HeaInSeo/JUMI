# JUMI Quality Guardrail Scorecard

Status: Current guardrail coverage map

This document tracks the quality guardrail categories JUMI intentionally
enforces. It is not a design wishlist; each row should either point at an
active guardrail or explicitly call out a remaining gap.

| Category | Status | Active guardrail |
|---|---|---|
| formatting, naming, static analysis | guarded | `make lint`, `.golangci.yml`, `go fmt`, `go vet`, `staticcheck`, `revive`, `misspell`, CI fmt drift check |
| security vulnerable pattern detection | guarded | custom Semgrep rules, kube-linter, manual `security-observe`, `lint-security`, blocking `make vuln-check` |
| complexity limit | guarded | `gocyclo` with `min-complexity: 35` in `.golangci.yml` |
| dependency and license checks | guarded with owner exceptions | `go mod tidy` drift check, blocking `make license-check`, and blocking `make vuln-check`; same-owner modules without license files are explicitly ignored until repository licenses are chosen |
| API usage rules | guarded | Semgrep rules for PodSpec `nodeName`, Pod watch selectors, Job delete UID preconditions, materialization env construction, failed ExecutionResult reasons |
| test coverage threshold | guarded | `make coverage-check`, `COVERAGE_THRESHOLD := 70`, uploaded coverage artifact |
| architecture layer dependency checks | guarded | `depguard` blocks direct Kubernetes imports outside backend-owned packages |

## Remaining Gap

The remaining dependency/license gap is owner-scoped. JUMI currently does not
have a root `LICENSE` file, and two same-owner modules in the dependency graph
also do not expose license files. The blocking scanner therefore ignores:

```text
github.com/HeaInSeo/JUMI
github.com/HeaInSeo/spawner
github.com/HeaInSeo/utils
```

Do not invent a license in automation; the project owner must choose the
repository license first. After that, remove these owner-scoped ignores as each
repository publishes an explicit license.

## Current Score

Guardrail score: 9.1 / 10

The score improved after adding:

- executable bad fixture matrix tests;
- smoke acceptance matrix documentation;
- Semgrep rule fixture tests;
- complexity limit in lint;
- quality guardrail scorecard;
- blocking third-party dependency license scan;
- `govulncheck` clean result after dependency and toolchain upgrades.

The score remains below 9.5 because same-owner license files and full
executable live smoke acceptance are not yet blocking CI.
