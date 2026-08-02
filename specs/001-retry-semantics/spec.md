# Feature Specification: Node-Level Execution Retry Semantics

**Feature Branch**: `001-retry-semantics`

**Created**: 2026-08-02

**Status**: Spec-anchored (reverse-specced from shipped behavior)

**Input**: Node-level execution retry semantics — `RetryPolicy.MaxAttempts` as a total-attempt cap, attempt lifecycle, failure propagation, and cancel interaction.

<!--
  This spec is REVERSE-SPECCED: it documents already-shipped behavior in the
  spec-kit format to (a) validate the pilot workflow end-to-end and (b) create
  the missing spec<->code<->gate linkage for a contract that previously drifted.
  Sources of truth this consolidates:
    - docs/JUMI_EXECUTABLE_RUN_SPEC_DRAFT.ko.md §9 (Retry Policy)
    - docs/JUMI_CANCEL_FAILURE_RETRY_SEMANTICS.ko.md §5-§6
  Implementation: pkg/executor/executor.go (RunE loop, failNode, retriesRemaining)
  Gate (tests): pkg/executor/dag_engine_lifecycle_test.go
-->

## User Scenarios & Testing *(mandatory)*

Actors: **pipeline authors** (set `retryPolicy` in the Executable Run Spec) and **operators** (observe attempts/events). JUMI executes; it does not decide backoff policy — that is an external layer (§14 of the run spec).

### User Story 1 - MaxAttempts is a total-attempt cap (Priority: P1)

A pipeline author sets `retryPolicy.maxAttempts` and expects it to bound the **total number of executions**, not the number of *extra* retries after the first.

**Why this priority**: This is the exact ambiguity that caused a shipped off-by-one (`retriesRemaining = MaxAttempts` → one spurious retry). It is the highest-value invariant to pin because it is a silent, correctness-affecting contract.

**Independent Test**: Configure a node whose execution always fails and assert the observed `AttemptCount`.

**Acceptance Scenarios**:

1. **Given** a node with `retryPolicy.maxAttempts = 1` whose execution fails, **When** the run executes, **Then** the node is executed exactly **1** time (0 retries) and ends `Failed`.
2. **Given** a node with `retryPolicy.maxAttempts = 2` whose execution always fails, **When** the run executes, **Then** the node is executed exactly **2** times (1 retry) and ends `Failed`.
3. **Given** a node with **no** `retryPolicy` (or `maxAttempts = 0`), **When** its execution fails, **Then** the node is executed exactly **1** time and ends `Failed` (no-retry default).

### User Story 2 - Attempt lifecycle and events (Priority: P1)

An operator observing a retried node must see each attempt as a distinct, terminal attempt with correct events, and the node must re-enter the ready path between attempts rather than being closed early.

**Why this priority**: Retry that silently reuses attempt identity or skips events makes failures un-debuggable and breaks provenance.

**Independent Test**: Run a node that fails once then would succeed; inspect attempts and the event stream.

**Acceptance Scenarios**:

1. **Given** a node that fails on attempt 1 with retries remaining, **When** the attempt fails, **Then** a `node.attempt_failed` event is emitted, a **new attempt ID** is issued, and the node re-enters the ready path (it is NOT closed `Failed`).
2. **Given** a node that exhausts all attempts, **When** the final attempt fails, **Then** a `node.failed` event is emitted and the node is terminal `Failed`.
3. **Given** a node that fails on attempt 1 and succeeds on attempt 2, **When** the run executes, **Then** the node is terminal `Succeeded` and the run may continue.

### User Story 3 - Cancel wins over remaining retry budget (Priority: P2)

**Why this priority**: A cancel that is silently overridden by a queued retry is a safety violation (zombie work after the operator asked to stop).

**Independent Test**: Cancel a run whose running node still has retry budget.

**Acceptance Scenarios**:

1. **Given** a run that has been accepted for cancel and a node with retries remaining, **When** the current attempt ends, **Then** **no new retry attempt** is created and the node/run settle terminal.
2. **Given** a run/node already in a terminal state, **When** cancel is requested, **Then** it is a no-op (terminal state not overwritten; no spurious `node.canceled`).

### Edge Cases

- `maxAttempts` set below 0 → rejected at validation time (`retryPolicy.maxAttempts must be >= 0`).
- Failure surfaced as a *result* (`!result.Succeeded`) rather than a Go error must still count as an attempt failure (see EXEC-002 invariant).
- Retry exhaustion sets failure reason `retry_exhausted` and then run failure policy (fail-fast) applies to downstream nodes.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001** (RETRY-001): `RetryPolicy.MaxAttempts` MUST be interpreted as a **total-attempt cap**. `maxAttempts = N` permits at most `N` executions (`N-1` retries). `maxAttempts = 1` permits exactly one execution and no retry. Source: `docs/JUMI_EXECUTABLE_RUN_SPEC_DRAFT.ko.md` §9.3. Implementation: `pkg/executor/executor.go` (`retriesRemaining = MaxAttempts - 1`).
- **FR-002**: Absent or zero `retryPolicy` MUST mean no-retry (single execution).
- **FR-003**: Each execution attempt MUST increment `AttemptCount` and carry a distinct attempt ID.
- **FR-004**: A non-final failed attempt MUST emit `node.attempt_failed`, MUST NOT close the node terminal, and MUST re-enter the ready path for the next attempt.
- **FR-005**: The final (budget-exhausted) failed attempt MUST emit `node.failed` and set the node terminal `Failed`.
- **FR-006**: A failure delivered as `!result.Succeeded` (nil Go error) MUST be treated as an attempt failure (EXEC-002).
- **FR-007**: Once a run's cancel is accepted, no further retry attempts MUST be created even if retry budget remains.
- **FR-008**: Cancel on an already-terminal run/node MUST be a no-op and MUST NOT emit `node.canceled`.
- **FR-009**: `maxAttempts < 0` MUST be rejected by spec validation.

### Key Entities

- **RetryPolicy**: `{ maxAttempts int, retryablePhases []string, retryDelayHint string }`. JUMI consumes only minimal execution semantics; backoff/classification is an external layer.
- **Attempt**: one execution of a node; has an attempt ID, a status (Prepared/Completed/Errored), and contributes to `AttemptCount`.
- **NodeRecord**: carries `Status`, `AttemptCount`, `CurrentAttemptID`, `TerminalFailureReason`.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: For `maxAttempts ∈ {0,1,2,3}` with an always-failing node, observed `AttemptCount` equals `max(1, maxAttempts)` in 100% of runs.
- **SC-002**: Every retried node produces exactly one `node.attempt_failed` per non-final failed attempt and exactly one terminal event (`node.failed` or `node.succeeded`).
- **SC-003**: Cancel-with-budget-remaining produces zero additional attempts in 100% of runs.
- **SC-004**: The above are enforced by a deterministic gate (unit tests under `-race`), not by manual review.

## Definition of Done

- [x] Behavior implemented in `pkg/executor/executor.go`.
- [x] FR-001/002 covered by `TestDagEngineNodeLevelRetryLoop` (`maxAttempts=2` → `AttemptCount==2`, terminal `Failed`, `node.attempt_failed` + `node.failed` present).
- [x] FR-007/008 covered by `TestDagEngineCancelIsNoOpOnAlreadyTerminalRunAndNode` and cancel scenarios.
- [x] FR-009 covered by `pkg/spec` validation tests (`TestValidateExecutableRunSpec_NegativeRetryMaxAttempts`).
- [ ] Bidirectional link added in `executor.go` pointing back to this spec (`specs/001-retry-semantics/spec.md`) — follow-up.

## Regression Guardrails

- The MaxAttempts total-cap semantics (FR-001) MUST remain covered by a test that fails if `retriesRemaining` reverts to `= MaxAttempts`. This guardrail exists because that exact off-by-one shipped once undetected.
- Attempt lifecycle events (FR-004/005) MUST remain asserted so a refactor cannot silently drop `node.attempt_failed`.

## Assumptions / Decisions

- **DECISION (resolves the RETRY-001 ambiguity)**: "total-attempt cap" is the ratified interpretation, per run-spec §9.3 and cancel/retry semantics §6.2 (`maxAttempts=2` → two total attempts). Had this spec + a requirement-critic pass existed earlier, the "cap vs retry-count" ambiguity would have been resolved at design time instead of surfacing as a shipped off-by-one.
- JUMI does not compute backoff/delay; `retryDelayHint`/`retryablePhases` are advisory to external layers.
- Failure-propagation to downstream nodes follows the run's `FailurePolicy` (fail-fast default), specified separately in `docs/JUMI_CANCEL_FAILURE_RETRY_SEMANTICS.ko.md` §4-§5.

## Implementation References

- `pkg/executor/executor.go` — `RunE` retry loop, `failNode`, `retriesRemaining = MaxAttempts - 1`.
- `pkg/executor/dag_engine_lifecycle_test.go` — `TestDagEngineNodeLevelRetryLoop`, cancel no-op test.
- `docs/JUMI_EXECUTABLE_RUN_SPEC_DRAFT.ko.md` §9; `docs/JUMI_CANCEL_FAILURE_RETRY_SEMANTICS.ko.md` §5-§6.
