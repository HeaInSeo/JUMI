# JUMI Registry Sync Policy

문서 상태: Draft v0.1  
작성일: 2026-05-30

## 목적

JUMI의 실행 정본은 GitHub source commit이지만, 실제 실행 환경은 registry image를 사용한다.  
이 문서는 GitHub, Harbor, GHCR 사이의 관계를 고정해서 source/runtime drift를 줄이기 위한 운영 기준을 정의한다.

핵심 원칙:

```text
GitHub is the source of truth.
Harbor is the primary runtime registry.
GHCR is the backup registry.
Harbor and GHCR must be synchronized for the same source commit when sync is enabled.
```

## 역할

```text
GitHub:
  source of truth

Harbor:
  primary runtime registry
  infra-lab / smoke / cluster runtime default

GHCR:
  backup registry
  audit / recovery / external reference
```

## 동기화 의미

이 문서에서 “동기화”는 아래를 의미한다.

```text
same source commit
  ↓
publish succeeds to Harbor
  and
publish succeeds to GHCR
```

v0에서는 “같은 source commit에서 두 registry publish가 모두 성공해야 한다”를 최소 기준으로 둔다.

주의:

- v0에서는 Harbor와 GHCR의 최종 digest identical 보장을 강제하지 않는다.
- 다만 두 registry publish는 같은 source commit, 같은 publish run 기준으로 수행되어야 한다.
- primary deploy는 Harbor ref를 사용한다.

## 현재 publish authority

publish authority와 smoke authority는 `100.123.80.48` remote VM이다.

```text
Local:
  code edit
  unit test
  CLI dry-run

Remote VM (100.123.80.48):
  Harbor/GHCR publish
  K8s smoke
  registry sync verification
```

## Publish 정책

### JUMI service image

- publish path: `scripts/publish-jumi-service-ko-remote.sh`
- primary publish: Harbor
- optional backup publish: GHCR
- sync enabled 시:
  - Harbor publish 성공
  - GHCR publish 성공
  - 둘 중 하나라도 실패하면 publish 실패

생성 artifact:

```text
artifacts/devspace/ko/jumi-service-image-ref.txt
artifacts/devspace/ko/jumi-service-image-ref.backup.txt
artifacts/devspace/ko/jumi-service-image-sync.json
```

### Runtime shortcut image / artifact-handoff image

- remote `podman build`
- sync enabled 시 primary/backup tag를 동시에 build
- Harbor push 후 GHCR push
- 둘 중 하나 실패하면 smoke 준비 실패

## Preflight 정책

sync enabled 시 preflight는 Harbor만 보면 안 된다.

확인 대상:

- Harbor reachability
- GHCR reachability
- remote Docker auth
- remote TLS connectivity

관련 스크립트:

```text
scripts/preflight-publish-env.sh
scripts/preflight-ko-remote.sh
```

## k8sgpt lint 정책

remote smoke 이후에는 `k8sgpt`를 lint 성격의 관찰 도구로 사용한다.

원칙:

- `k8sgpt` 결과는 JSON artifact로 저장한다.
- smoke 성공/실패와 별개로 문제 관찰 근거를 남긴다.
- `k8sgpt`가 설치되어 있지 않으면 optional mode에서는 skip 가능하다.

기본 artifact 경로:

```text
artifacts/devspace/k8sgpt/jumi-ah-dev-live-smoke.json
```

## runtime alignment 확인

runtime alignment는 최소한 아래를 확인해야 한다.

1. JUMI `Containerfile`이 참조하는 `node-artifact-runtime` pin
2. 실제 `node-artifact-runtime` GitHub main/head
3. smoke가 사용하는 Harbor runtime image가 어떤 source commit에서 빌드되었는지

정렬(alignment)을 수행할 때는:

```text
1. pin update
2. remote publish
3. Harbor/GHCR sync 확인
4. smoke 재실행
5. k8sgpt JSON lint 확인
```

## Verification Commands

사람이 같은 검증을 재현할 수 있어야 한다.

### Local preflight

```bash
make preflight-publish-local
```

### Remote preflight

```bash
make preflight-publish-remote
make preflight-ko-remote
```

### Remote service publish

```bash
SYNC_BACKUP_REGISTRY=true \
BACKUP_REGISTRY_HOST=ghcr.io \
BACKUP_KO_DOCKER_REPO=ghcr.io/HeaInSeo \
make ko-publish-remote
```

### Same-node local_reuse smoke

```bash
SYNC_BACKUP_REGISTRY=true \
BACKUP_REGISTRY_HOST=ghcr.io \
./scripts/run-jumi-same-node-local-reuse-live-smoke.sh
```

### simple_http remote_fetch smoke

```bash
SYNC_BACKUP_REGISTRY=true \
BACKUP_REGISTRY_HOST=ghcr.io \
./scripts/run-jumi-remote-fetch-simple-http-live-smoke.sh
```

