# JUMI New Backend Scope Decision

Date: 2026-06-01

## Decision

The deferred "new backend" item is not a request to add another artifact
transport backend, and it is not the bori operator work.

For the current JUMI/AH/nan track, "new backend" means a JUMI execution backend
cleanup: make the Kubernetes/nan execution path a first-class backend while
keeping the existing spawner compatibility path until the nan runtime image is
fully rolled out.

## Responsibility Boundary

JUMI owns:

- DAG execution and node attempt lifecycle
- AH `ResolveBinding` calls before scheduling and after actual Pod scheduling
- Kubernetes Job execution for a node attempt
- per-attempt events, terminal status, output metadata collection

AH owns:

- artifact/source metadata persistence
- source selection and binding decisions
- credential-free persistent metadata policy

nan owns:

- node runtime contract execution
- input materialization policy
- digest/size verification at runtime
- output manifest generation inside the node container

bori will own later:

- install/operator concerns
- dataplane app rollout and version management
- operational gates beyond shift-left validation
- fleet-level observability and reconciliation loops

## Backend Scope

In scope for the JUMI new backend track:

- Extract the direct Kubernetes/nan path from `pkg/backend/spawner_k8s.go` into a
  clearer first-class execution backend.
- Keep `backend.Adapter` stable unless a concrete API gap is found.
- Keep `spawner` compatibility while `jumi-P2-2` remains deferred.
- Preserve `wrapped-shell` and `runtime-helper` compatibility branches until the
  nan runtime image rollout is complete.
- Keep current Kueue observation and Pod scheduled-node observation behavior.
- Keep `make verify-sprint-3d-baseline` as the baseline regression gate.

Out of scope for this track:

- bori operator implementation
- Dev Space or deployment remnant cleanup
- transport backends such as Dragonfly, peer fetch, or external fetch commands
- signed URL broker implementation
- cleanup/TTL/eviction controller implementation

## Proposed Migration Steps

1. Introduce backend selection naming in config/docs without changing defaults.
2. Move pure Kubernetes Job construction helpers behind a direct backend
   boundary, keeping current tests.
3. Make the nan node-contract path the explicit direct backend path.
4. Leave spawner compatibility as a separate adapter path.
5. Remove legacy wrapped-shell/runtime-helper branches only after `jumi-P2-2`
   preconditions are met.

## Rollback Rule

The rollback path is to keep the existing `SpawnerK8sAdapter` behavior as the
default until the direct backend is proven by:

- `go test ./pkg/backend ./pkg/executor ./pkg/handoff`
- `make verify-sprint-3d-baseline`
- remote same-node local-reuse smoke
- remote simple HTTP fetch smoke

## Risks

- Prematurely removing spawner compatibility can break existing dev and smoke
  fixtures.
- Moving operator/deployment behavior into JUMI would conflict with the bori
  direction.
- Changing the `backend.Adapter` interface too early can force unnecessary
  executor changes.

## Conclusion

The next implementation sprint should be a narrow JUMI backend decomposition
step, not a new operational controller. bori integration remains a later
control-plane/operator track.
