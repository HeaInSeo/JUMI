# JUMI simple_http Artifact Source v0 Plan

> 작성일: 2026-05-22  
> 상태: Draft v0.1  
> 목적: `remote_fetch` v0 happy path에서 fetchable `http://...` URI를 어떻게 공급할지 작게 고정한다.

---

## 1. 문제 정의

현재 live chain은 아래까지는 된다.

1. `nan`이 output manifest 생성
2. JUMI가 manifest 회수
3. JUMI가 AH에 `RegisterArtifact`
4. AH가 `ResolveBinding`
5. JUMI가 `MaterializationPlan`을 env로 전달

하지만 현재 runtime artifact URI는 기본적으로 `jumi://...` logical URI다.

즉 현재 상태에서는:

- unit-level contract smoke는 가능
- live `remote_fetch` decision seam도 가능
- 그러나 VM-facing materialization smoke에서 바로 fetch 가능한 `http://...` URI는 아직 공급되지 않음

이 문서는 그 gap을 v0 범위에서 메우기 위한 작은 설계 문서다.

---

## 2. 결정 사항

### 2.1 `nan`은 fetchable URI의 SOT가 아니다

- `nan`은 output manifest를 만들 수 있다
- `nan`은 logical artifact URI를 포함할 수 있다
- 하지만 특정 환경에서 어떤 fetch source를 쓸지 결정하지 않는다
- Dragonfly, object storage, peer endpoint, 사내 CLI 같은 정책을 `nan`이 직접 소유하면 안 된다

### 2.2 `jumi://...`는 logical URI로 유지할 수 있다

예:

```text
jumi://run/{runId}/node/{nodeId}/attempt/{attemptId}/output/{name}
```

이 URI는 artifact의 논리적 식별자다.
반드시 fetch 가능한 URI일 필요는 없다.

### 2.3 장기적인 fetchable URI SOT

장기적으로 fetchable URI의 source of truth는 아래 계층이 소유해야 한다.

- AH
- 또는 AH 뒤의 Materialization Source Layer / Artifact Source Registry

하지만 이 계층은 이번 v0 스프린트에서 구현하지 않는다.

### 2.4 v0 happy path는 `simple_http` artifact source를 사용한다

이것은 최종 전송 구조가 아니라, `remote_fetch` materialization smoke를 빠르게 닫기 위한 검증용 backend다.

원칙:

- pipeline spec에는 `simple_http`를 넣지 않는다
- `simple_http` 선택은 runtime/materialization profile 또는 VM test profile에서만 이뤄진다
- AH는 등록된 fetchable URI를 `MaterializationPlan.URI`로 pass-through 한다
- JUMI는 지금처럼 env/runtime context로 전달한다
- B runtime helper가 그 URI를 GET해서 digest를 검증하고 `/work/inputs/<inputName>`에 배치한다

---

## 3. Logical URI 와 Fetchable URI

예시:

```yaml
logicalUri: jumi://run/{runId}/node/{nodeId}/attempt/{attemptId}/output/{name}
fetchableUri: http://artifact-source.local/artifacts/{digest}
digest: sha256:...
```

해석:

- `logicalUri`
  - artifact identity / internal reference
- `fetchableUri`
  - 실제 fetch 가능한 transport URI
- `digest`
  - fetch 결과 검증 기준

v0 happy path에서는 AH에 등록되는 URI가 `fetchableUri` 역할을 하게 한다.
logical URI와 fetchable URI를 별도 필드로 분리하는 것은 v1 이후 설계 후보로 남긴다.

---

## 4. v0 후보안 비교

### Option A. `nan` manifest가 직접 `http://...` URI를 쓴다

설명:

- A runtime helper가 output manifest에 처음부터 fetch 가능한 `http://...` URI를 기록

장점:

- JUMI/AH 경로를 덜 건드릴 수 있다

단점:

- `nan`이 fetch source 정책을 알게 된다
- runtime helper가 환경 종속적으로 무거워진다
- 장기 설계와 맞지 않는다

판단:

- 채택하지 않음

### Option B. JUMI core가 `RegisterArtifact` 직전에 URI를 직접 override 한다

설명:

- JUMI core가 등록 직전에 `jumi://...`를 `http://...`로 덮어쓴다

장점:

- v0 구현은 쉬울 수 있다

단점:

- JUMI core가 fetch source policy를 갖는 것처럼 보인다
- 장기 책임 경계가 흐려진다

판단:

- 문서상 기본 방향으로 채택하지 않음

### Option C. v0 VM happy path 전용 runtime/materialization profile adapter가 fetchable URI를 공급한다

설명:

- A output을 simple HTTP server root 아래에 노출하는 test/runtime profile adapter를 둔다
- 이 adapter가 AH에 들어갈 fetchable `http://...` URI를 공급한다
- AH는 그 URI를 그대로 `MaterializationPlan.URI`로 pass-through 한다

장점:

- `nan`을 가볍게 유지할 수 있다
- JUMI core를 전송 정책으로 오염시키지 않는다
- v0 happy path를 작게 닫기 좋다

단점:

- test/runtime profile adapter라는 별도 개념이 필요하다

판단:

- v0 권장안

---

## 5. v0 권장안

v0에서는 Option C를 따른다.

즉:

1. A node가 output을 생성한다
2. `nan`은 logical URI와 digest를 포함한 manifest를 만든다
3. v0 VM happy path용 runtime/materialization profile adapter가 A output을 simple HTTP source 아래에 노출한다
4. AH에 등록되는 URI는 fetch 가능한 `http://...` URI다
5. AH는 이를 `MaterializationPlan.URI`로 pass-through 한다
6. JUMI는 이를 B env/runtime context로 전달한다
7. B runtime helper가 `simple_http`로 fetch하고 digest를 검증한다

---

## 6. simple_http Artifact Source 최소 요구사항

v0에서는 아주 작은 기준만 요구한다.

- 단일 파일 GET
- 무인증
- range request 없음
- redirect 없음 또는 최소화
- 파일 크기 `1MB -> 10MB -> 100MB`
- digest는 `sha256`

이 경로는 최종 object storage strategy를 정하는 것이 아니라, materialization contract를 검증하기 위한 baseline이다.

---

## 7. B runtime helper 책임

v0 `simple_http` materializer 최소 책임:

- `MaterializationPlan.URI` 읽기
- `expectedDigest` 읽기
- HTTP GET
- 임시 파일 저장
- sha256 계산
- digest 비교
- `/work/inputs/<inputName>`로 atomic move
- 실패 시 non-zero exit

---

## 8. Sprint 연결

이 문서는 아래 스프린트와 직접 연결된다.

- Sprint 1
  - unit-level contract smoke는 `jumi://...` logical URI로 닫음
  - VM-facing materialization smoke는 fetch 가능한 `http://...` URI가 필요
- Sprint 2
  - `nan` simple_http materializer 최소 구현
- Sprint 3
  - VM happy path e2e

---

## 9. 비범위

이번 v0 문서에서 다루지 않는 것:

- Dragonfly backend 구현
- peer fetch backend 구현
- node-local CAS cache
- auth-enabled object store
- fetch retry / resume / range request
- AH 내부 복잡한 source registry
- logical URI와 fetchable URI의 정식 분리 필드

---

## 10. 다음 작업

다음 작업은 아래 순서가 적절하다.

1. Sprint 1 잔여분
   - VM-facing materialization smoke에 필요한 `http://...` URI 공급 경로를 profile adapter 관점으로 확정
2. Sprint 2
   - `nan` simple_http materializer 최소 구현
3. Sprint 3
   - VM happy path 검증
