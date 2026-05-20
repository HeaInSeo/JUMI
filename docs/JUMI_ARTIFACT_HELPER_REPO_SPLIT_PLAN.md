# JUMI Artifact Helper Repo Split Plan

> 작성일: 2026-05-16
> 목적: `node-artifact-runtime` helper와 JUMI/AH 연동 계약 문서를 JUMI repository 밖의 별도 GitHub repository로 분리하는 방향을 고정한다.

## 1. 한 줄 결정

`node-artifact-runtime` helper와 runtime contract 문서는 JUMI repository 안에서 장기 운영하지 않는다.

장기적으로는 별도 GitHub repository에서 독립적으로 개발/버전관리/배포한다.

별도 repo 이름은 아래로 고정한다.

- `github.com/HeaInSeo/node-artifact-runtime`

현재 helper source scaffold와 연동 문서의 초기 이관은 이미 위 repo에서 시작했다.

## 2. 왜 별도 repo 인가

이 helper는:

- JUMI service process의 일부가 아니다
- AH service process의 일부가 아니다
- 여러 tool image가 공통으로 상속할 runtime contract 자산이다

즉 소속이 JUMI repository 안에 고정되면 경계가 흐려진다.

별도 repo로 분리하는 이유:

- tool/runtime 관점의 독립 버전 관리
- JUMI service release와 helper release 분리
- 다른 프로젝트에서 공통 runtime base image를 재사용 가능
- helper와 runtime base image를 JUMI/AH와 별도 lifecycle로 운영 가능

## 3. 의도된 repo 역할

별도 repo가 소유할 것:

- `node-artifact-runtime` source
- helper binary build
- JUMI/AH integration contract docs
- contract tests

JUMI repo가 계속 소유할 것:

- JUMI service binary
- execution coordination
- helper path/env contract 소비
- manifest readback
- AH registration path

AH repo가 계속 소유할 것:

- artifact inventory / handoff resolution
- finalize / GC / lifecycle seam

NodeKit / NodeVault 계열 repo가 장기적으로 소유할 것:

- node runtime base image definition
- runtime base image publish pipeline
- tool image inheritance examples

## 4. JUMI 관점 경계

JUMI는 helper repo의 producer가 아니라 consumer다.

즉 JUMI는 아래만 안다.

- helper path contract
- helper가 쓰는 manifest contract
- helper가 유지해야 하는 env contract

JUMI가 알 필요 없는 것:

- helper image build internals
- runtime base image layer construction
- tool image packaging details

## 5. 현재 저장소에서 유지할 것

분리 전환기에는 JUMI repo에 compatibility 경로가 남을 수 있다.

예:

- JUMI 저장소 안의 helper source
  - `cmd/jumi-output-helper`
  - `pkg/runtimehelper`
  는 제거 완료 상태를 유지한다
- smoke shortcut fixture
- legacy helper path

하지만 이들은 장기 소속을 의미하지 않는다.

즉 현재 저장소에 남아 있는 helper 관련 항목은:

- migration compatibility
- contract stabilization
- smoke/dev support

용도라고 해석해야 한다.

## 6. 분리 이후 의도된 소비 방식

JUMI는 장기적으로 다음을 소비한다.

- published helper binary or image
- documented runtime contract version
- NodeKit / NodeVault가 문서에 맞춰 만든 published base image

예:

```text
helper repo
  - release: node-artifact-runtime:v0.x
  - documents runtime contract for base-image builders
```

```text
NodeVault / NodeKit output
  - publishes runtime base image compatible with the documented contract
```

```text
tool repo
  FROM <published runtime base image>
  installs bwa or gatk or samtools
```

```text
JUMI repo
  consumes helper path / manifest contract only
```

## 7. migration 단계

1. JUMI repo 안에서 helper boundary 문서화
2. helper path/env contract 안정화
3. 별도 GitHub repo 생성
4. helper source와 contract docs 이동
5. NodeVault / NodeKit에서 representative base image를 구현
6. representative tool image를 새 base image 기준으로 검증
7. JUMI smoke fixture에서 JUMI image shortcut 제거
8. JUMI repo 안의 helper compatibility code 제거 여부 판단

## 8. open items

- helper release tagging policy
- base image naming policy
- cross-repo contract versioning strategy
- JUMI/AH integration tests가 어느 repo에 살지 결정

## 9. 현재 판단

이 방향에서 가장 중요한 원칙은 하나다.

`node-artifact-runtime`은 JUMI의 부속 바이너리가 아니라, node runtime contract를 담당하는 별도 GitHub repo 자산이 되어야 한다.

TODO(supply-path): public source fetch is acceptable for current smoke validation, but the long-term supply path should move beyond source fetch.
- GitHub Release binary
- OCI image
- Harbor mirror
- ORAS artifact
- digest-pinned runtime base image
