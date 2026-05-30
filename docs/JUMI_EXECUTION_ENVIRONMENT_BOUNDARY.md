# JUMI Execution Environment Boundary

문서 상태: Draft v0.1  
작성일: 2026-05-18

## 목적

JUMI 개발에서는 코드 수정 위치와 실제 publish/smoke 기준 위치가 다르다.
이 문서는 그 경계를 repo 규칙으로 고정해서, 로컬에서 가능한 작업과
`100.123.80.48` K8s VM 환경에서만 해야 하는 작업을 구분한다.

핵심 원칙:

```text
Local build is not publish authority.
Remote VM is the publish and smoke authority.
GitHub is the source of truth.
Harbor is the primary runtime registry.
GHCR is the backup registry.
```

한국어로는:

```text
로컬 빌드는 CLI 확인용이다.
이미지 publish와 K8s smoke의 기준 환경은 100.123.80.48이다.
```

## 실행 위치 매트릭스

| 작업 | 허용 위치 | 비고 |
|---|---|---|
| 코드 수정 | local | 가능 |
| `go test` / unit test | local | 가능 |
| `nan` CLI dry-run | local | 가능 |
| runtime image build | local or remote | local build는 검증용 |
| Harbor reachability check | local or remote | push 전에 반드시 확인 |
| Harbor push | `100.123.80.48` | local 금지 |
| runtime image publish | `100.123.80.48` | remote 기준 |
| K8s smoke | `100.123.80.48` | remote 기준 |
| Pod/Job runtime 검증 | `100.123.80.48` | remote 기준 |

## 왜 이 경계가 필요한가

현재 JUMI 작업은 단일 로컬 프로젝트가 아니다.

```text
로컬 개발 머신
  - 코드 수정
  - go test
  - unit test
  - image build 일부 가능

100.123.80.48 / K8s VM 환경
  - Harbor 접근 가능
  - image push 가능
  - K8s smoke 가능
  - Pod/Job runtime 검증 가능
```

따라서 아래 두 개를 같은 의미로 취급하면 안 된다.

```text
1. podman build 성공
2. Harbor push / K8s smoke 가능
```

`build`, `push`, `cluster pull + run`은 서로 다른 단계다.

## 운영 규칙

다음 규칙을 기본으로 한다.

1. 로컬 개발 머신에서는 코드 수정, unit test, `nan` CLI dry-run까지만 수행한다.
2. Harbor push, runtime image publish, K8s smoke, Pod/Job 검증은 반드시 `100.123.80.48`에서 수행한다.
3. 로컬에서 `podman build`가 성공해도 publish 가능하다고 가정하지 않는다.
4. `harbor.10.113.24.96.nip.io` reachability는 publish/smoke 전 preflight로 확인한다.
5. `SYNC_BACKUP_REGISTRY=true`일 때는 Harbor와 GHCR publish가 모두 성공해야 한다.
6. push/smoke를 수행하기 전 현재 호스트가 기준 환경인지 확인하고, 아니면 중단하고 보고한다.

## Preflight 규칙

publish 또는 smoke 전에는 아래를 실행한다.

```text
make preflight-publish-local
make preflight-publish-remote
```

해석:

- local preflight는 현재 호스트에서 Harbor에 닿는지 확인한다.
- remote preflight는 `100.123.80.48`에서 Harbor에 닿는지 확인한다.

remote preflight가 성공하고 local preflight가 실패하는 것은 비정상이 아니다.
오히려 현재 JUMI 운영 구조에서는 expected behavior일 수 있다.

## Target 이름 규칙

이름 자체로 실행 위치를 드러내야 한다.

좋은 예:

```text
make runtime-build-local
make runtime-check-local
make runtime-smoke-remote
```

피해야 할 예:

```text
make build
make push
make smoke
```

## Agent 작업 규칙

Codex나 다른 에이전트에게 JUMI/runtime image 작업을 시킬 때는 아래 블록을 함께 준다.

```text
환경 경계 규칙:

- 로컬 개발 머신에서는 코드 수정, unit test, nan CLI dry-run까지만 수행한다.
- Harbor push, runtime image publish, K8s smoke, Pod/Job 검증은 반드시 100.123.80.48 환경에서 수행한다.
- 로컬에서 podman build가 성공해도 publish 가능하다고 가정하지 않는다.
- harbor.10.113.24.96.nip.io 접근성은 작업 시작 전에 실행 위치에서 preflight로 확인한다.
- push/smoke를 수행하기 전 현재 호스트가 기준 환경인지 확인하고, 아니면 중단하고 보고한다.
```

## 현재 단계의 의미

이 문서는 runtime image refresh 같은 작업에서 다음 순서를 강제하기 위해 추가한다.

```text
local phase:
  code / unit / dry-run

remote phase:
  publish / smoke / K8s verification
```
