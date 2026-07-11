# JUMI Quality Guardrail Scorecard

Status: Current guardrail coverage map

This document tracks the quality guardrail categories JUMI intentionally
enforces. It is not a design wishlist; each row should either point at an
active guardrail or explicitly call out a remaining gap.

| Category | Status | Active guardrail |
|---|---|---|
| formatting, naming, static analysis | guarded | `make lint`, `.golangci.yml`, `go fmt`, `go vet`, `staticcheck`, `revive`, `misspell`, CI fmt drift check |
| security vulnerable pattern detection | guarded | custom Semgrep rules, kube-linter, manual `security-observe`, `lint-security`, `govulncheck` observation |
| complexity limit | guarded | `gocyclo` with `min-complexity: 35` in `.golangci.yml` |
| dependency and license checks | partial | `go mod tidy` drift check and `govulncheck`; dependency license scanning is not yet blocking |
| API usage rules | guarded | Semgrep rules for PodSpec `nodeName`, Pod watch selectors, Job delete UID preconditions, materialization env construction, failed ExecutionResult reasons |
| test coverage threshold | guarded | `make coverage-check`, `COVERAGE_THRESHOLD := 70`, uploaded coverage artifact |
| architecture layer dependency checks | guarded | `depguard` blocks direct Kubernetes imports outside backend-owned packages |

## Remaining Gap

Dependency license scanning is the main known gap. JUMI currently does not have
a root `LICENSE` file or a dependency license scanner wired into blocking CI.
Do not invent a license in automation; the project owner must choose the
repository license first. After that, add a pinned license scanner or an
explicit dependency license allowlist.

## Current Score

Guardrail score: 8.8 / 10

The score improved after adding:

- executable bad fixture matrix tests;
- smoke acceptance matrix documentation;
- Semgrep rule fixture tests;
- complexity limit in lint;
- quality guardrail scorecard.

The score remains below 9.5 because dependency license scanning and full
executable live smoke acceptance are not yet blocking CI.
