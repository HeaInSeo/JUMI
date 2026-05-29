# Sprint 3C-3

Security / Integrity Guardrails for Artifact Source Registry

> 작성일: 2026-05-27  
> 상태: Reviewed Baseline  
> 상위 문서: [Artifact Source Registry / Materialization Source Layer](./JUMI_ARTIFACT_SOURCE_REGISTRY_DESIGN.md)  
> 범위: Artifact Source Registry / Materialization Source Layer의 보안·무결성 보강

---

## 1. 한 줄 요약

3C-3은 Artifact Source Registry가 잘못된 source, 조작된 path, 신뢰할 수 없는 location, stale source, digest mismatch를 materialization candidate로 사용하지 않도록 막는 보안·무결성 보강 문서다.

## 2. 왜 3C-3이 필요한가

Sprint 3C는 다음 모델을 도입한다.

- `ArtifactRecord`
- `ArtifactSourceRecord`
- `SourceBackendRegistry`
- `SourceLocation` typed union
- `SourceState`
- `ResolveBinding.materializationCandidates[]`

source가 여러 개가 되면 단순히 “source가 있다”만으로는 부족하다.

반드시 확인해야 할 것:

- 이 source는 정말 `ready` 상태인가?
- 이 source의 backend는 `enabled` 상태인가?
- 이 source의 location type은 backend type과 맞는가?
- 이 path는 허용된 root 아래인가?
- 이 digest는 `expectedDigest`와 일치하는가?
- 이 candidate는 target node 조건을 만족하는가?
- 이 source가 credential이나 내부 정보를 log로 노출하지 않는가?

즉 3C-3의 목적은 source registry 모델이 확장될 때 생길 수 있는 보안·무결성 문제를 미리 막는 것이다.

## 3. 위협 모델

### 3.1 신뢰 경계

- `JUMI`
  - DAG 실행과 Pod contract 전달을 담당
  - source registry 정본은 아님
- `AH`
  - ArtifactRecord / SourceRecord / ResolveBinding 정본
- `nan`
  - 선택된 materialization plan 실행
  - source selection은 하지 않음
- `user command`
  - 신뢰하지 않음
  - `/work` 내부 파일 수정 가능하다고 가정
- `source location payload`
  - 외부 입력 또는 이전 단계 출력에서 파생될 수 있으므로 검증 대상

원칙:

`user command`와 `source location payload`는 기본적으로 untrusted로 본다.

### 3.2 주요 위험

1. 잘못된 `NodeLocalPath`가 전달되어 의도하지 않은 파일을 읽음
2. `stale` / `unreachable` / `deleted` source가 candidate로 선택됨
3. backend type과 `SourceLocation` type이 불일치함
4. digest가 맞지 않는 content가 정상 artifact처럼 사용됨
5. `logicalUri`를 fetch URL처럼 오해함
6. HTTP source가 외부 임의 URL로 확장됨
7. credential이 source record나 log에 노출됨
8. hardlink/reflink 최적화가 CAS 원본 무결성을 깨뜨림
9. candidate priority가 비결정적이라 실행마다 다른 source를 선택함
10. contract/env 주입 시 input name/path가 escape됨

## 4. 보안 원칙

1. `SourceRecord`는 신뢰하지 말고 검증한다.
2. `SourceLocation`은 typed union으로 정확히 하나만 허용한다.
3. `backendId`와 `location` type은 반드시 일치해야 한다.
4. `ready` 상태 source만 candidate가 될 수 있다.
5. digest 검증은 materialization의 최종 방어선이다.
6. `node_local` path는 허용된 root 아래만 허용한다.
7. `logicalUri`는 URL이 아니라 opaque identity다.
8. credential은 `SourceRecord`에 직접 저장하지 않는다.
9. candidate priority는 deterministic해야 한다.
10. `local_reuse`는 `copy default`를 유지한다.

## 5. SourceLocation 검증

### 5.1 typed union 강제

정확히 하나의 backend location만 허용한다.

허용:

```yaml
location:
  nodeLocal:
    nodeName: worker-2
    path: /jumi-node-artifacts/cas/sha256/abc123
```

허용:

```yaml
location:
  http:
    uri: http://artifact-source.local/artifacts/abc123
```

거부:

```yaml
location:
  nodeLocal:
    nodeName: worker-2
    path: /jumi-node-artifacts/cas/sha256/abc123
  http:
    uri: http://artifact-source.local/artifacts/abc123
```

### 5.2 backend/type 일치

검증 규칙:

- `backend.type == node_local` → `location.nodeLocal` 필수
- `backend.type == http` → `location.http` 필수
- `backend.type == object_store` → `location.objectStore` 필수

## 6. NodeLocalPath 보안 기준

### 6.1 허용 root

node-local source path는 반드시 허용된 artifact root 하위여야 한다.

예:

```text
allowedRoot = /jumi-node-artifacts
```

허용:

- `/jumi-node-artifacts/cas/sha256/abc123`

거부:

- `/etc/passwd`
- `/jumi/node-contract.json`
- `../../etc/passwd`
- `/tmp/random-file`

### 6.2 검증 방식

권장:

1. path가 absolute인지 확인
2. `filepath.Clean` 적용
3. allowedRoot도 `filepath.Clean` 적용
4. `filepath.Rel(allowedRoot, path)` 계산
5. `rel`이 `..`로 시작하면 거부
6. `rel == "."`도 파일 경로로는 거부 가능

### 6.3 symlink 정책

Sprint 3C에서는 보수적으로 간다.

`node_local` materialization에서 source path가 symlink이면 기본 거부한다.

후속에서 symlink를 허용하려면:

- `EvalSymlinks` 후에도 allowedRoot 하위인지 확인
- CAS path canonicalization
- symlink 생성 주체 제한

## 7. Digest 검증 정책

### 7.1 expectedDigest 필수

모든 materialization candidate는 `expectedDigest`를 가져야 한다.

### 7.2 planner 단계와 materializer 단계 분리

planner 단계:

- `source.digest == artifact.digest` 검증

materializer 단계:

- 실제 bytes에 대한 `expectedDigest` 검증

### 7.3 path를 digest 대신 신뢰하지 않는다

`/cas/sha256/<hex>` 경로는 힌트다. 최종 무결성은 materialization 후 digest 검증으로 확인한다.

## 8. Source State 기준

### 8.1 candidate 허용 state

`ready`만 candidate 가능.

거부:

- `pending`
- `stale`
- `unreachable`
- `deleted`

### 8.2 deleted source 기본 제외

`ListSources` 기본 응답에서는 `deleted` source를 제외한다.

### 8.3 stale source

Sprint 3C v0에서는 candidate로 사용하지 않는다.

후속에서 degraded candidate로 허용할 수는 있지만 현재는 금지한다.

## 9. HTTP Source 기준

### 9.1 URI credential policy

기본 정책:

- URI `userinfo`는 무조건 거부한다.
- query string은 3C-3E 기준으로 기본 거부한다.
- signed URL 지원은 future work로 미루며, 이번 스프린트에서는 예외 플래그를 두지 않는다.

거부 예:

- `https://user:pass@example.com/file`
- `https://example.com/file?token=secret`
- `https://example.com/file?X-Amz-Signature=...`

후속 메모:

- signed URL을 지원하려면 저장 정책, redaction 정책, runtime-only short-lived 사용 정책을 별도로 정의해야 한다.

### 9.2 허용 scheme

개발 환경:

- `http` 허용 가능

운영 환경:

- `https` 권장

금지:

- `file://`
- `ftp://`
- `ssh://`
- `gopher://`

### 9.3 redirect 정책

기본값:

- redirect 제한 또는 비활성화

허용 시:

- 최대 redirect 횟수 제한
- redirect 후 host allowlist 재검증
- scheme downgrade 금지

### 9.4 host allowlist

운영 환경에서는 HTTP source host를 제한한다.

기본 정책:

- allowlist가 비어 있으면 `remote_fetch/http` source는 거부한다.
- 개발 편의를 위한 `allow any`는 명시적 opt-in이 있을 때만 허용한다.

예:

- `artifact-source.jumi-system.svc`
- `artifact-cache.jumi-system.svc`

dev/test profile에서는 explicit override로 완화할 수 있다.

### 9.5 size limit

가능하면 `expectedSizeBytes`를 검증한다.

정책:

- `Content-Length`가 `expectedSizeBytes`보다 크면 거부
- 다운로드 완료 후 실제 size 확인
- size 미상인 경우 `maxInputBytes` 정책 적용

## 10. Credential 처리 기준

### 10.1 SourceRecord에 secret 직접 저장 금지

거부:

```yaml
location:
  http:
    uri: https://...
    headers:
      Authorization: Bearer abc123
```

권장:

```yaml
credentialRef:
  kind: secret
  name: artifact-source-token
  key: token
```

### 10.2 log redaction

로그 금지:

- `Authorization` header
- signed URL query
- secret name/key 전체
- credentialRef value
- temporary token

debug log에도 동일 정책 적용.

## 11. ResolveBinding candidate 기준

### 11.1 candidate 생성 전 검증

- `source.state == ready`
- `backend.enabled == true`
- backend capability가 mode와 일치
- `source.digest == artifact.digest`
- `SourceLocation` type이 backend type과 일치
- required condition을 만들 수 있음

### 11.2 deterministic priority

권장 순서:

1. `local_reuse`
2. `remote_fetch`
3. `dragonfly_pull`
4. `object_fetch`
5. `peer_fetch`

동일 입력에서는 항상 같은 결과가 나와야 한다.

### 11.3 local_reuse condition 필수

`local_reuse` candidate는 반드시 `scheduled_on_node` 조건을 포함해야 한다.

### 11.4 source_state_ready condition

candidate에는 source 상태 조건도 넣는다.

## 12. JUMI contract / env 주입 기준

### 12.1 inputName sanitize

권장 규칙:

```text
^[A-Za-z0-9_.-]+$
```

### 12.2 localPath 고정

candidate가 내려주는 `localPath`는 `/work/inputs` 하위여야 한다.

planner는 safe default `localPath`만 생성하고, JUMI/nan은 defense-in-depth로 다시 검증한다.

### 12.3 contract log 제한

허용:

- `inputName=result`
- `mode=local_reuse`
- `sourceId=src-...`
- `digest=sha256:abc123...`

주의:

- full URI
- headers
- credentialRef
- signed URL
- 민감한 local absolute path

## 13. local_reuse copy default 정책

`BUG-2`는 correctness bug가 아니라 zero-copy 최적화 후보로 재분류했다.

3C-3 기준:

- `local_reuse`는 `copy default`를 유지한다.

이유:

- CAS 원본 보호
- consumer input 격리
- `/work/inputs` 수정이 CAS 원본을 오염시키지 않음

금지:

- hardlink-first 기본 적용
- symlink-first 기본 적용
- digest 검증 생략

후속 후보:

- `reflink optional`
- `hardlink explicit opt-in`
- `copy fallback`

## 14. 감사 로그 / 이벤트 기준

권장 이벤트:

- `artifact.source.added`
- `artifact.source.state_changed`
- `artifact.source.rejected`
- `artifact.resolve.candidate_generated`
- `artifact.resolve.candidate_rejected`
- `artifact.materialization.digest_mismatch`
- `artifact.materialization.path_rejected`

로그 허용:

- `runId`
- `nodeId`
- `attemptId`
- `artifactId`
- `sourceId`
- `backendId`
- `mode`
- `state`
- `reason`

로그 금지:

- secret
- token
- `Authorization` header
- signed URL 전체
- 민감한 local absolute path

## 15. 구현 대상 전체 목록

1. `SourceLocation` typed union 검증
2. backend/type mismatch 거부
3. `node_local` allowedRoot 검증
4. symlink source path 기본 거부
5. `ready` 상태만 candidate 허용
6. `deleted` source 기본 제외
7. `stale` / `unreachable` source candidate 제외
8. `source.digest == artifact.digest` 검증
9. candidate `expectedDigest` 필수화
10. `local_reuse` candidate의 `scheduled_on_node` 조건 강제
11. `source_state_ready` condition 추가
12. HTTP scheme allowlist
13. HTTP host allowlist
14. HTTP redirect 제한
15. HTTP size limit / expected size 검증
16. credential direct embed 금지
17. contract/log redaction
18. unsafe `inputName` 거부 또는 normalize
19. `localPath`를 `/work/inputs` 하위로 제한
20. `local_reuse copy default` 유지

## 16. 단계적 구현 스프린트

20개 항목은 단순 번호 순서가 아니라 같은 write scope와 회귀 범위를 공유하는 단위로 5개씩 묶는다.

### Sprint 3C-3A

AH source admission / planner 최소 검증

1. `SourceLocation` typed union 검증
2. backend/type mismatch 거부
3. `ready` 상태만 candidate 허용
4. `stale` / `unreachable` / `deleted` source candidate 제외
5. `source.digest == artifact.digest` 검증

특징:

- `artifact-handoff` 단일 write scope
- 저장 모델과 planner 필터링만 건드리므로 회귀 범위가 작다

### Sprint 3C-3B

node_local path / contract 경계 검증

1. `node_local` allowedRoot 검증
2. symlink source path 기본 거부
3. unsafe `inputName` 거부 또는 normalize
4. `localPath`를 `/work/inputs` 하위로 제한
5. candidate `expectedDigest` 필수화

특징:

- `node-artifact-runtime`과 `JUMI` contract/env 경계로 나뉜다
- 같은 스프린트 안에서 repo write scope별 병렬 구현이 가능하다

### Sprint 3C-3C

HTTP source 최소 정책

1. HTTP scheme allowlist
2. HTTP host allowlist
3. HTTP redirect 제한
4. HTTP size limit / expected size 검증
5. unsupported / disallowed source를 candidate에서 거부

특징:

- `artifact-handoff` planner 정책과 `node-artifact-runtime` fetcher 정책을 병렬로 다룰 수 있다
- 상태:
  - Completed
- 구현 반영:
  - `artifact-handoff` candidate planner의 HTTP source validation
  - `node-artifact-runtime` remote_fetch의 scheme / allowlist / redirect / expected size guardrail
  - `JUMI` contract의 `expectedSizeBytes` 전달

### Sprint 3C-3D

credential / logging / policy hardening

1. credential direct embed 금지
2. contract/log redaction
3. `deleted` source 기본 조회 제외
4. `local_reuse copy default` 정책 고정
5. 감사 로그 / 이벤트 기준 정리

특징:

- 문서화와 구현이 함께 필요한 최종 hardening 단계다
- 상태:
  - Completed
- 구현 반영:
  - `artifact-handoff` SourceRecord의 HTTP headers direct embed 거부
  - `JUMI` handoff client의 HTTP header redaction
  - `artifact-handoff` deleted source 기본 조회 제외
  - `node-artifact-runtime`의 safe input name normalize 유지
  - `local_reuse copy default` 정책 유지

### Sprint 3C-3E

Guardrail closure / contract parity

1. HTTP/gRPC contract parity 정리
2. `logicalUri`와 materialization source 의미 분리 강제
3. fail-open 기본값 제거
4. timeout / size / digest / path 검증 fail-closed화
5. repo별 최소 regression verification command 고정

상태:

- Completed

구현 반영:

- `artifact-handoff` gRPC parity
  - `logicalUri`
  - `locations`
  - `expectedSizeBytes`
  - `sourceLocation`
  - `localPath`
  - `materializationCandidates`
- `artifact-handoff`가 `logicalUri`를 synthetic `local_reuse` source로 사용하지 않도록 planner hardening
- `artifact-handoff`와 `node-artifact-runtime`의 HTTP URI credential 정책
  - `userinfo` reject
  - query reject
- `node-artifact-runtime`의 default HTTP timeout
- `node-artifact-runtime`의 `EXPECTED_SIZE_BYTES` parse fail-closed
- `node-artifact-runtime`의 `local_reuse expected size` 검증
- `node-artifact-runtime`의 node-local realpath containment 강화
- `JUMI`의 gRPC parity 수용
- `JUMI`의 binding env key collision fail-fast

## 17. Verification Commands

repo별 최소 재현 검증 명령은 다음과 같다.

`artifact-handoff`

```bash
env TMPDIR=/dev/shm/go-tmp-ah GOCACHE=/dev/shm/go-build-ah GOROOT=/usr/local/go /usr/local/go/bin/go test ./pkg/domain ./pkg/resolver
```

`node-artifact-runtime`

```bash
env TMPDIR=/dev/shm/go-tmp-nan GOCACHE=/dev/shm/go-build-nan GOROOT=/usr/local/go /usr/local/go/bin/go test ./pkg/runtimehelper ./cmd/node-artifact-runtime
```

`JUMI`

```bash
env TMPDIR=/dev/shm/go-tmp-jumi GOCACHE=/dev/shm/go-build-jumi GOROOT=/usr/local/go /usr/local/go/bin/go test ./pkg/handoff ./pkg/executor
```

## 18. 3C 본문에 넣을 짧은 문구

Security / Integrity Guardrails

Sprint 3C의 Source Registry 모델은 source location을 그대로 신뢰하지 않는다. `ArtifactSourceRecord`는 candidate 생성 전에 state, backend capability, typed location, digest, path boundary를 검증해야 한다. `node_local` source는 allowed artifact root 하위 경로만 허용하고, `local_reuse`는 CAS 원본 보호를 위해 `copy default`를 유지한다. HTTP source는 scheme, host, redirect, size 제한을 적용할 수 있어야 한다. credential은 `SourceRecord`에 직접 저장하지 않고 `credentialRef`로만 참조한다.

## 19. 최종 결론

3C-3은 새로운 기능 문서가 아니다.

3C에서 source registry가 들어오면서 생길 수 있는 path traversal, stale source 사용, digest mismatch, credential leakage, backend/location mismatch, unsafe `local_reuse` 최적화를 막기 위한 보안·무결성 가드레일 문서다.

한 줄 결론:

3C 본문은 “무엇을 만들 것인가”를 정의하고, 3C-3은 “잘못된 source를 절대 믿지 않기 위한 안전 기준”을 정의한다.
