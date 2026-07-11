# JUMI Dependency License Guardrail

Status: Active for third-party dependencies

JUMI runs a blocking dependency license check with:

```sh
make license-check
```

The check uses the repository script:

```text
hack/license-guardrails.sh
```

The script is intentionally small and based on `go list -deps` for `./cmd/jumi`,
because external license scanners can lag Go toolchain metadata changes. It
checks each runtime dependency module root for a license-like file and
classifies the text against the policy below.

## Policy

The blocking check allows these third-party license families:

```text
Apache-2.0
MIT
BSD
ISC
```

It rejects dependencies with license text classified as:

```text
forbidden
unknown
```

Forbidden text includes GPL, LGPL, AGPL, MPL, EPL, CDDL, and Creative Commons
families. Unknown means no license-like file was found or the license text did
not match the explicit allowlist.

## Owner-Scoped Exceptions

The following prefixes are ignored by `hack/license-guardrails.sh` for now:

```text
github.com/HeaInSeo/JUMI
github.com/HeaInSeo/spawner
github.com/HeaInSeo/utils
```

These are owner-scoped exceptions, not general license approvals. They exist
because JUMI currently has no root `LICENSE` file and two same-owner modules in
the dependency graph also do not expose license files. Do not widen this list
without documenting why the ignored module is owner-controlled.

## Remaining Work

Choose and add a root JUMI repository license. After that, remove the
`github.com/HeaInSeo/JUMI` ignore. The same should be done for `spawner` and
`utils` after those repositories publish explicit license files.
