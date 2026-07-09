# JUMI Kubernetes Job Label Contract

Status: Canonical

This document defines the Kubernetes Job and Pod template metadata contract for
JUMI node attempts.

## Identity Labels

Every Kubernetes Job created for a node attempt, and the Job's Pod template,
must carry the same JUMI identity labels:

```text
jumi.io/run-key
jumi.io/node-key
jumi.io/attempt-id
jumi.io/workload-role
```

These labels are selector contract fields. They are not optional debug
metadata. JUMI uses them to identify, observe, and clean up run/node/attempt
workloads.

The original long identifiers are preserved as annotations:

```text
jumi.io/run-id
jumi.io/node-id
jumi.io/attempt-marker
```

The spawner runtime ownership marker may be longer than the Kubernetes label
value limit. The full marker must stay in `jumi.io/attempt-marker`; any
compatibility label must be Kubernetes-label-safe.

## User Labels

User-provided labels must live under:

```text
user.jumi.io/
```

The following prefixes are reserved for JUMI, Spawner, Kubernetes app identity,
and integration controllers:

```text
jumi.io/
spawner.io/
app.kubernetes.io/
kueue.x-k8s.io/
```

Reserved prefix conflicts must fail fast before a Job is submitted.

## Kueue Integration Label

Kueue queue selection is an integration contract, not a user label. JUMI may
generate:

```text
kueue.x-k8s.io/queue-name
```

only from explicit Kueue hints. When this label is present, the Kubernetes Job
is created with `spec.suspend=true` so Kueue controls admission before the Job
controller creates Pods.

## Scheduling Contract

JUMI expresses hard node placement through `requiredNodeName` and
`nodeSelector`. `requiredNodeName` materializes to:

```text
nodeSelector["kubernetes.io/hostname"]
```

If `requiredNodeName` conflicts with an explicit
`nodeSelector["kubernetes.io/hostname"]`, validation must fail fast.

JUMI expresses soft artifact locality through preferred node affinity. Preferred
placement is an optimization hint, not a hard scheduling guarantee.
