# JUMI Remote Fetch v0 Sprint Plan

> 작성일: 2026-05-22  
> 상태: Active  
> 목적: `remote_fetch` / artifact materialization v0 범위를 고정하고, 작업이 중단되더라도 같은 기준에서 재개할 수 있게 한다.

구현 상태 업데이트:

- Sprint 3A `remote_fetch` happy path 완료
- Sprint 3B same-node `local_reuse` happy path 완료
- Sprint 3C-1 / 3C-2 source registry 저장 모델 및 candidate planner 완료
- Sprint 3C-3A / 3C-3B / 3C-3C / 3C-3D / 3C-3E guardrail hardening 완료
- 현재 문서의 초기 v0 설명 일부는 보존하되, 최신 기준선은 위 상태 업데이트를 우선한다

---

## 1. 한 줄 요약

현재 JUMI / AH / nan 기준선은 다음과 같다.

- `contract smoke`는 가능하다.
- `real materialization smoke`는 아직 미구현이다.
- v0의 첫 실제 backend는 `simple_http`로 제한한다.
- v0의 임시 materialization target path는 `/work/inputs/<inputName>`으로 둔다.

즉 현재 상태는:

```text
AH가 MaterializationPlan(mode, uri, expectedDigest)를 반환한다
↓
JUMI가 이를 env/runtime context로 전달한다
↓
runtime helper가 실제 fetch/materialize하는 구현은 아직 완성되지 않았다
```

---

## 2. 용어 구분

### 2.1 Contract Smoke

다음을 검증하는 단계다.

- A output manifest 생성
- JUMI `RegisterArtifact`
- B 준비 시 `ResolveBinding`
- AH가 `remote_fetch + uri + expectedDigest` 반환
- JUMI가 이를 B env/runtime context에 주입

이 단계에서는 B가 실제로 파일을 fetch하지 않아도 된다.

중요:

- unit-level contract smoke에서는 `jumi://...` logical URI 전달까지만 확인해도 된다.
- VM-facing materialization smoke에서만 fetch 가능한 `http://...` URI를 확인한다.

### 2.2 Materialization Smoke

다음을 검증하는 단계다.

- B runtime helper가 실제로 artifact를 가져온다
- digest를 검증한다
- `/work/inputs/<inputName>`에 atomic하게 준비한다
- B command가 그 파일을 읽고 성공한다

---

## 3. 현재 기준선

### 3.1 현재 가능한 것

- AH가 `MaterializationPlan.mode`, `uri`, `expectedDigest`를 반환
- JUMI가 이를 env/runtime context로 전달
- `remote_fetch` decision 계측
- A -> B live smoke에서 contract seam 자체는 검증 가능

### 3.2 현재 불가능한 것

- runtime helper가 `uri`를 실제로 fetch
- digest mismatch 시 fetch 단계에서 terminal fail
- `/work/inputs/<inputName>` materialization
- large-file transport backend 검증

### 3.3 v0 transport backend 결정

v0 happy path backend는 `simple_http`로 제한한다.

제한 범위:

- 단일 파일 GET
- 무인증 또는 테스트용 단순 HTTP
- sha256 검증
- small file happy path 우선

backlog로 미루는 것:

- `dragonfly`
- `node_peer_fetch`
- `external_command`
- `direct_object_store` 일반화
- object storage auth / signed URL
- retry / resume / range request

### 3.4 v0 materialization target path

임시 기준 경로:

```text
/work/inputs/<inputName>
```

이 경로는 v0 happy path를 닫기 위한 임시 contract다.
장기적으로 `/in` 또는 별도 runtime contract path로 다시 정리될 수 있다.

### 3.5 URI 계층

v0 기준으로 URI는 두 층으로 본다.

- logical URI
  - 예: `jumi://run/{runId}/node/{nodeId}/attempt/{attemptId}/output/{name}`
  - artifact identity 또는 내부 식별자 성격
- fetchable URI
  - 예: `http://artifact-source.local/artifacts/{digest}`
  - 실제 fetch 가능한 transport URI

원칙:

- `nan`은 fetchable URI의 SOT가 아니다.
- 장기적으로 fetchable URI의 SOT는 AH 또는 그 뒤의 materialization source layer가 맡는다.
- 하지만 v0에서는 복잡한 source registry를 만들지 않고, VM happy path용 `simple_http` artifact source를 둔다.

---

## 4. 스프린트 개요

| Sprint | 목표 | 상태 |
|---|---|---|
| Sprint 0 | 기준선 고정 | Completed |
| Sprint 1 | contract smoke 안정화 | Completed |
| Sprint 2 | `nan` simple_http materializer 최소 구현 | Completed |
| Sprint 3A | VM consumer materialization happy path | Completed |
| Sprint 3B | pure K8s same-node local_reuse handoff | Completed |
| Sprint 4 | 정리와 backlog 고정 | Completed |
| Sprint 3C-1 | Artifact Source Registry 저장 모델 | Completed |
| Sprint 3C-2 | materialization candidate planner | Completed |
| Sprint 3C-3A | AH source admission / planner 최소 검증 | Completed |
| Sprint 3C-3B | node_local path / contract 경계 검증 | Completed |
| Sprint 3C-3C | HTTP source 최소 정책 | Completed |
| Sprint 3C-3D | credential / logging / policy hardening | Completed |
| Sprint 3C-3E | guardrail closure / contract parity | Completed |
| Sprint 3C-4A | node-contract input baseline | Completed |
| Sprint 3D-1 | AddSource lifecycle minimal API | Completed |
| Sprint 3D-2 | source verifier minimal API | Completed |
| Sprint 3D-3 | verification baseline target | Completed |

### 4.1 Sprint 3C-3E 완료 메모

범위:

- HTTP/gRPC contract parity 완전 동등화
- `logicalUri` / env contract / source location 의미 고정
- fail-open 기본값 제거
- timeout / size / digest / path 검증 fail-closed화
- release gate 이전의 최소 regression test 추가

의도적으로 하지 않은 것:

- post-scheduling re-resolve
- AddSource lifecycle 확장
- `node-contract.json`
- cleanup / TTL
- 새 backend
- hardlink / reflink 최적화

Verification commands:

```bash
env TMPDIR=/dev/shm/go-tmp-ah GOCACHE=/dev/shm/go-build-ah GOROOT=/usr/local/go /usr/local/go/bin/go test ./pkg/domain ./pkg/resolver
env TMPDIR=/dev/shm/go-tmp-nan GOCACHE=/dev/shm/go-build-nan GOROOT=/usr/local/go /usr/local/go/bin/go test ./pkg/runtimehelper ./cmd/node-artifact-runtime
env TMPDIR=/dev/shm/go-tmp-jumi GOCACHE=/dev/shm/go-build-jumi GOROOT=/usr/local/go /usr/local/go/bin/go test ./pkg/handoff ./pkg/executor
```

### 4.2 Sprint 3C-4A 완료 메모

범위:

- `JUMI_NODE_CONTRACT_JSON`에 materialization input spec 직렬화
- `node-artifact-runtime`이 contract `inputs[]`를 env보다 우선 사용
- runtime-helper mode에서 `JUMI_INPUT_*_{URI,EXPECTED_DIGEST,EXPECTED_SIZE_BYTES,MATERIALIZATION_MODE,NODE_LOCAL_PATH,LOCAL_PATH}` env 제거
- contract input `expectedSizeBytes >= 0` validation 추가

Verification commands:

```bash
env TMPDIR=/tmp/jumi-node-contract-tmp GOCACHE=/tmp/jumi-node-contract-cache GOROOT=/usr/local/go /usr/local/go/bin/go test ./pkg/backend ./pkg/executor
env TMPDIR=/tmp/nan-node-contract-tmp GOCACHE=/tmp/nan-node-contract-cache GOROOT=/usr/local/go /usr/local/go/bin/go test ./pkg/contract ./pkg/runtimehelper ./cmd/node-artifact-runtime
```

### 4.3 Sprint 3D-1 완료 메모

범위:

- `artifact-handoff`에 `AddSource`, `UpdateSourceState`, `ListSources` 최소 API 추가
- HTTP/gRPC parity 유지
- artifact/source lookup을 위한 store direct lookup 추가
- `AddSource` 시 artifact digest / source digest / typed location guardrail 재사용
- `UpdateSourceState`는 source state 변경만 허용하고 background verifier는 포함하지 않음

의도적으로 하지 않은 것:

- JUMI에서 `AddSource` 호출하는 orchestration
- source verifier / health checker
- post-scheduling re-resolve
- cleanup / TTL

Verification commands:

```bash
env TMPDIR=/tmp/ah-addsource-tmp GOCACHE=/tmp/ah-addsource-cache GOROOT=/usr/local/go /usr/local/go/bin/go test ./pkg/domain ./pkg/inventory ./pkg/resolver
```

### 4.4 Sprint 3D-2 완료 메모

범위:

- `artifact-handoff`에 `VerifySource` 최소 API 추가
- source record에 `lastVerifiedAt`, `lastError` 필드 추가
- verifier는 기존 source guardrail을 재사용해 source를 `ready` 또는 `unreachable`로 갱신
- HTTP/gRPC parity 유지

의도적으로 하지 않은 것:

- background verifier loop
- scheduler/post-scheduling re-resolve 연동
- cleanup / TTL
- source freshness 정책 자동화

Verification commands:

```bash
env TMPDIR=/tmp/ah-addsource-tmp GOCACHE=/tmp/ah-addsource-cache GOROOT=/usr/local/go /usr/local/go/bin/go test ./pkg/domain ./pkg/inventory ./pkg/resolver
```

### 4.5 Sprint 3D-3 완료 메모

범위:

- JUMI / artifact-handoff / node-artifact-runtime focused verification command를 하나의 baseline 타깃으로 묶음
- runtime alignment check와 handoff proto sync check를 verification baseline에 포함
- full CI wiring은 하지 않고, 사람 기준 재현 가능한 release-adjacent 검증 명령만 고정

의도적으로 하지 않은 것:

- GitHub Actions / CI wiring
- remote live smoke 자동 실행
- post-scheduling re-resolve
- cleanup / TTL

Verification commands:

```bash
make verify-sprint-3d-baseline
make runtime-align-check
```

---

## 5. Sprint 0 — 기준선 고정

목표:

- 현재 상태를 `contract smoke 가능 / real materialization 미구현`으로 명확히 고정
- `remote_fetch`를 특정 다운로드 기술이 아니라 materialization 동작으로 정의
- `simple_http`를 v0 backend로 결정
- `/work/inputs/<inputName>`를 임시 target path로 결정

산출물:

- 현재 `remote_fetch` 상태 문서화
- `contract smoke`와 `materialization smoke` 용어 분리
- v0 happy path backend를 `simple_http`로 결정
- 임시 materialization target path 결정

완료 기준:

- 문서에 현재 가능/불가능 범위가 명확함
- 구현 범위가 `simple_http`로 제한됨
- `dragonfly` / `peer_fetch`가 backlog로 이동함

상태:

- 완료

관련 문서:

- [JUMI Node Runtime Artifact Contract](./JUMI_NODE_RUNTIME_ARTIFACT_CONTRACT.md)
- [JUMI AH nan Integration Review](./JUMI_AH_NAN_INTEGRATION_REVIEW.md)
- [JUMI Design](./JUMI_DESIGN.ko.md)
- [JUMI Locality Semantics Review](./JUMI_LOCALITY_SEMANTICS_REVIEW.ko.md)

---

## 6. Sprint 1 — Contract Smoke 안정화

목표:

- 실제 fetch 전 단계까지 확실히 검증

검증 흐름:

```text
A 실행
  ↓
A output manifest 생성
  ↓
JUMI가 AH RegisterArtifact 호출
  ↓
B 준비 시 ResolveBinding 호출
  ↓
AH가 remote_fetch + uri + expectedDigest 반환
  ↓
JUMI가 B env에 주입
```

확인할 env:

- `JUMI_INPUT_<X>_URI`
- `JUMI_INPUT_<X>_MATERIALIZATION_MODE`
- `JUMI_INPUT_<X>_EXPECTED_DIGEST`
- `JUMI_INPUT_<X>_SOURCE_NODE`
- `JUMI_INPUT_<X>_PLACEMENT_MODE`

완료 기준:

- B Job/Pod 실행 환경에 `remote_fetch` env가 들어감
- `expectedDigest`가 비어 있지 않음
- 이 단계에서는 B가 fetch하지 않아도 됨
- B runtime image가 아직 fetch를 하지 않아도 contract smoke는 성공으로 본다

예상 작업:

- JUMI/AH fixture가 실제 `uri`를 가진 `MaterializationPlan`을 안정적으로 만들도록 정리
- smoke summary에서 `contract smoke`와 `materialization smoke`를 구분
- fetchable URI 공급 경로를 v0 VM happy path용 runtime/materialization profile adapter 관점으로 고정

현재 기준 상태:

- executor 단위 테스트에서는 `status`, `decision`, `uri`, `source_node`, `placement_mode`, `materialization_mode`, `expectedDigest`, `requires_materialization` env 주입을 고정할 수 있다
- unit-level contract smoke에서는 `jumi://...` logical URI 전달을 허용한다
- live smoke에서는 `remote_fetch` decision seam과 계측은 검증되지만, fetch 가능한 `http://...` URI까지는 아직 고정되지 않았다
- 이후 Sprint 3A에서 VM-facing materialization smoke까지 검증되었으므로 Sprint 1은 완료로 본다

관련 설계 메모:

- [JUMI simple_http Artifact Source v0 Plan](./JUMI_SIMPLE_HTTP_ARTIFACT_SOURCE_V0_PLAN.md)

---

## 7. Sprint 2 — nan simple_http materializer 최소 구현

목표:

- B runtime에서 실제 파일을 가져오고 digest를 검증

현재 상태:

- `node-artifact-runtime`에서 env 기반 `remote_fetch` input 파싱이 구현됨
- `simple_http` GET + `sha256` digest 검증 + `/work/inputs/<inputName>` atomic move가 단위 테스트로 검증됨
- child command에는 `JUMI_INPUT_<X>_LOCAL_PATH` env가 주입됨
- 현재 GitHub pin 기준선: `github.com/HeaInSeo/node-artifact-runtime@v0.1.5`
- 아직 VM happy path용 fetchable `http://...` URI 공급 adapter는 미구현

최소 구현:

- env 또는 contract에서 `uri` 읽기
- `expectedDigest` 읽기
- `simple_http` GET으로 파일 다운로드
- 임시 파일에 저장
- sha256 계산
- `expectedDigest`와 비교
- 성공 시 `/work/inputs/<inputName>`으로 atomic move
- 실패 시 non-zero exit

지원 backend:

- `simple_http`

지원 digest:

- `sha256`

지원 파일 크기:

- `1MB ~ 10MB`

권장 구현 디테일:

- temp path 예: `/work/.jumi-fetch/<inputName>.part`
- digest mismatch 시 최종 target path는 만들지 않음
- 같은 inputName의 덮어쓰기 정책은 v0에서는 "성공 시 최종 파일 교체"로 단순화

완료 기준:

- B가 실제로 파일을 다운로드함
- digest mismatch 시 실패함
- digest 일치 시 `/work/inputs/<inputName>`에 파일이 생김
- B command가 그 파일을 읽고 성공함
- 구현이 GitHub 정본 `node-artifact-runtime`에 반영되어 JUMI 쪽이 commit/tag로 pin 가능함

---

## 8. Sprint 3A — VM Consumer Materialization Happy Path 검증

목표:

- 실제 VM/kind 환경에서 consumer remote_fetch materialization path를 end-to-end로 확인

시나리오 이름:

- `simple_http_remote_fetch_A_to_B`

흐름:

1. pre-seeded `simple_http` artifact source가 deterministic `1MiB` artifact를 제공
2. A node는 happy-path 전용 manual manifest를 직접 작성
3. manual manifest에는 fetch 가능한 `http://...` URI와 `sha256` digest가 들어감
4. JUMI가 기존 방식으로 AH에 `RegisterArtifact`
5. AH는 등록된 `http://...` URI를 `MaterializationPlan.URI`로 pass-through
6. B가 `ResolveBinding` 결과로 `remote_fetch` plan 수신
7. JUMI가 B env에 `URI`, `expectedDigest`, `materialization_mode`, `local_path`를 전달
8. `nan@v0.1.5` runtime helper가 `simple_http`로 fetch
9. digest 검증
10. `/work/inputs/<inputName>` 준비
11. B가 input 읽고 성공
12. 전체 run `Succeeded`

Sprint 3A 범위:

- 이것은 consumer materialization smoke다
- producer가 실제 artifact를 HTTP source에 upload/publish하는 검증은 포함하지 않는다
- JUMI core `FetchableUriMapper` 추가는 포함하지 않는다
- AH Artifact Source Registry 추가는 포함하지 않는다

파일 크기 단계:

- 1단계: `1MB`
- 2단계: `10MB`
- 3단계: `100MB`

완료 기준:

- A Job/Pod 성공
- AH `RegisterArtifact` 성공
- B `ResolveBinding remote_fetch` 확인
- B fetch 성공
- digest 검증 성공
- B 성공
- 전체 run 성공

Sprint 3A 실제 결과:

- `simple_http` source service가 cluster 내부에서 접근 가능함
- AH `RegisterArtifact`에 `http://simple-http-artifact-source/artifacts/dataset-1mb.bin` URI가 기록됨
- `ResolveBinding` 이후 consume env에 아래 값이 존재함
  - `JUMI_INPUT_DATASET_URI`
  - `JUMI_INPUT_DATASET_MATERIALIZATION_MODE=remote_fetch`
  - `JUMI_INPUT_DATASET_EXPECTED_DIGEST`
  - `JUMI_INPUT_DATASET_LOCAL_PATH=/work/inputs/dataset`
- `nan@v0.1.5`가 실제 HTTP fetch, sha256 검증, `/work/inputs/dataset` materialization을 수행함
- consume command가 materialized input을 읽고 성공함
- run `Succeeded`

판정 원칙:

- Sprint 3A `remote_fetch` materialization smoke 성공 여부는 run/materialization evidence로 먼저 판정한다
- `slint-gate` policy evaluation은 post-smoke gate로 분리한다
- local VM run에서는 `SLINT_GATE_MODE=optional` 또는 `skip`을 허용한다
- CI 또는 release gate에서는 `SLINT_GATE_MODE=required`로 강제할 수 있다

Smoke harness 정리 상태:

- `remote smoke`, `summary generation`, `slint-gate`는 로그와 exit code에서 분리됐다
- `SLINT_GATE_MODE=optional|required|skip`를 지원한다
- local VM run에서는 `slint-gate` binary 부재가 smoke 실패로 간주되지 않는다
- CI/release gate에서는 `SLINT_GATE_MODE=required`로 강제할 수 있다

현재 판정:

- 1MiB 단계는 완료
- 10MiB 단계는 완료
- 100MiB 단계는 완료

### Sprint 3B — pure K8s same-node local_reuse handoff

Sprint 3B의 목적은 외부 upload path가 아니라 pure K8s same-node local handoff를 검증하는 것이다.

핵심:

- producer-side node-local artifact promotion
- same-node `local_reuse` materialization
- `hostPath`는 validation mechanism으로만 사용

happy path:

```text
A Pod on worker-2
  ↓
A가 artifact 생성
  ↓
node-local artifact area CAS path로 copy promotion
  ↓
manifest에 logicalUri + producerAttemptId + digest + node_local location 기록
  ↓
JUMI RegisterArtifact
  ↓
AH ResolveBinding
  ↓
B Pod도 worker-2에 배치
  ↓
B가 local_reuse로 /work/inputs에 copy materialization
```

완료된 선행 작업:

- JUMI가 output manifest의 `logicalUri`, `producerAttemptId`, `locations[]`를 보존하고 `RegisterArtifact` HTTP seam으로 전달
- AH HTTP resolver가 `logicalUri`, `locations[]`, `node_local sourceLocation`, relative `localPath`를 저장/반환
- `node-artifact-runtime`이 producer-side node-local CAS promotion과 consumer-side `LocalReuseMaterializer`를 수행
- directK8s path가 `hostpath:` mount를 `HostPathVolumeSource`로 번역

검증 결과:

- same-node local_reuse live smoke 성공
- producer는 `copy into CAS path` 방식으로 `/jumi-node-artifacts/cas/sha256/<digest>`에 promotion
- AH 등록 artifact에는 `logicalUri`와 `locations[].nodeLocal.path`가 기록
- consume env/log에서 아래가 확인됨
  - `JUMI_INPUT_RESULT_MATERIALIZATION_MODE=local_reuse`
  - `JUMI_INPUT_RESULT_NODE_LOCAL_PATH=/jumi-node-artifacts/cas/sha256/<digest>`
  - `JUMI_INPUT_RESULT_LOCAL_PATH=/work/inputs/result`
- consume는 same worker node에서 `/work/inputs/result`로 copy materialization 후 성공
- 전체 run `Succeeded`

Sprint 3B success run:

- runId: `jumi-ah-dev-live-smoke-20260525T073733Z`
- sampleRunId: `jumi-ah-dev-live-smoke-sample-20260525T073733Z`

Sprint 3B local_reuse stress:

- `1GiB` same-node local_reuse stress 성공
- runId: `jumi-ah-dev-live-smoke-20260525T074939Z`
- sampleRunId: `jumi-ah-dev-live-smoke-sample-20260525T074939Z`
- producer artifact:
  - `sizeBytes=1073741824`
  - `digest=sha256:49bc20df15e412a64472421e13fe86ff1c5165e18b2afccf160d4dc19fe68a14`
- consume는 `mode=local_reuse`, `nodeLocalPath=/jumi-node-artifacts/cas/sha256/<digest>`, `localPath=/work/inputs/result`로 성공
- 현재 확인된 same-node local_reuse happy-path 범위는 `1GiB`까지다

현재 한계 판정:

- `1GiB` same-node local_reuse까지는 현재 경로에서 병목 없이 통과했다
- 아직 검증하지 않은 것은 `multi-GiB` stress, cleanup/disk pressure, retry/resume, remote fallback이다

관련 문서:

- [Sprint 3B Design: Pure K8s Node-local Handoff Happy Path](./JUMI_SPRINT_3B_PURE_K8S_NODE_LOCAL_HANDOFF.md)
- [Artifact Source Registry / Materialization Source Layer](./JUMI_ARTIFACT_SOURCE_REGISTRY_DESIGN.md)
- live smoke wrapper: [scripts/run-jumi-same-node-local-reuse-live-smoke.sh](../scripts/run-jumi-same-node-local-reuse-live-smoke.sh)

참고:

- `100MB`까지 통과하면 현재 단계에서는 충분하다
- `1GB+`는 다음 안정화 단계로 미룬다

---

## 9. Sprint 4 — 정리와 Backlog 고정

목표:

- v0에서 닫은 것과 남은 부채를 분명히 남긴다

backlog:

- `dragonfly` backend
- `node_peer_fetch`
- `external_command` backend
- node-local CAS cache
- retry / resume
- large-file stress test
- post-scheduling `ResolveBinding`
- directK8s placement parity
- `required_node` pending timeout policy
- auth-enabled object store backend
- multi-input concurrent fetch policy
- cache eviction policy
- Artifact Source Registry / Materialization Source Layer
- 1GiB+ remote_fetch stress
- multi-GiB same-node local_reuse stress
- full gate environment standardization (`slint-gate` packaging / availability)

완료 기준:

- v0에서 닫은 것과 안 닫은 것이 분명함
- 다음 개발자가 `remote_fetch` 상태를 오해하지 않음
- `simple_http` happy path 결과가 문서에 남음

상태:

- 완료
- `remote_fetch` happy path와 `same-node local_reuse` happy path를 각각 독립적으로 문서화했다.
- smoke 실행과 `slint-gate` 정책 평가는 분리됐다.
- `BUG-1`, `BUG-4`, `BUG-5`, `BUG-6` 안정화 패치와 `BUG-3` producer promotion single-pass 최적화가 반영됐다.
- `BUG-2`는 Sprint 3B v0의 의도된 `copy` 정책과 충돌하므로 후속 materializer strategy 설계 항목으로 남긴다.
- `BUG-2` 후속 전략 기본값은 `copy default / reflink optional / hardlink explicit opt-in / copy fallback`으로 기록한다.

---

## 10. 재개 규칙

이 문서를 기준으로 중단/재개 시 아래 순서로 확인한다.

1. 현재 스프린트 상태 확인
2. `contract smoke`와 `materialization smoke`를 혼동하지 않았는지 확인
3. v0 backend가 여전히 `simple_http`로 제한되어 있는지 확인
4. materialization target path가 `/work/inputs/<inputName>`인지 확인
5. backlog 항목이 범위 안으로 새어 들어오지 않았는지 확인

재개 시 첫 확인 질문:

```text
지금 우리는 Sprint 2 simple_http materializer를 GitHub 정본에 pin하는 중인가,
아니면 Sprint 3 VM happy path adapter로 넘어갔는가?
```

---

## 11. 비범위 항목

이번 v0 스프린트에서 기본값으로 밀지 않는 것:

- Dragonfly 필수 통합
- long-running sidecar
- PV/PVC 중심 공유 스토리지
- generic PodSpec extension
- full post-scheduling placement/materialization re-resolve

---

## 12. 현재 판정

현재 문서 기준 판정은 아래와 같다.

- Sprint 0: 완료
- Sprint 1: 완료
- Sprint 2: 완료
- Sprint 3A: 완료
- Sprint 3B: 완료

즉 지금은 "consumer remote_fetch materialization happy path와 pure K8s same-node local_reuse handoff happy path가 모두 닫혔다"가 정확한 상태다.

다음 활성 범위:

- Sprint 3C-1: Artifact Source Registry 저장 모델 도입 — Completed
- Sprint 3C-2: materialization candidate planner 도입 — Completed
- Sprint 3C-3: Security / Integrity Guardrails 문서화 — Draft Addendum
  - Sprint 3C-3A: AH source admission / planner 최소 검증 — Completed
  - Sprint 3C-3B: node_local path / contract 경계 검증 — Completed
  - Sprint 3C-3C: HTTP source 최소 정책 — Completed
  - Sprint 3C-3D: credential / logging / policy hardening — Completed

## 13. directK8s / library freeze 메모

### 13.1 directK8s placement parity

이번 정리 라운드에서 닫은 것:

- directK8s fallback path도 `RequiredNodeName`을 `kubernetes.io/hostname` nodeSelector로 반영한다
- directK8s fallback path도 `PreferredNodes`를 `preferredDuringSchedulingIgnoredDuringExecution` nodeAffinity로 반영한다

즉 `RequiredNodeName` / `PreferredNodes` 번역은 일반 spawner K8s driver semantics와 맞췄다.

여전히 backlog로 남는 것:

- post-scheduling `ResolveBinding(targetNodeName=actualNode)`
- `required_node` Pending/Unschedulable timeout policy 정교화
- directK8s path와 일반 spawner path의 전체 behavioral parity 장기 검토

### 13.2 library drift freeze

현재 JUMI가 사용하는 버전:

- `github.com/HeaInSeo/dag-go v1.2.0`
- `github.com/HeaInSeo/spawner v0.2.1`
- `github.com/HeaInSeo/node-artifact-runtime@bee9f999c242180f3f447591b54ab511a8413ae5`

GitHub main drift:

- `dag-go` main은 현재 JUMI pin `v1.2.0`와 일치한다
- `spawner` main은 현재 JUMI pin `v0.2.1`와 일치한다
- `node-artifact-runtime`은 `v0.1.5` tag 이후 추가 커밋을 JUMI가 commit pin으로 소비하고 있다

이번 스프린트 원칙:

- drift는 문서로 고정한다
- `dag-go`, `spawner`는 이번 라운드에서 GitHub 정본 최신으로 맞췄다
- `node-artifact-runtime`은 다음 tag 정리 전까지 commit pin 상태를 유지한다
- 외부 라이브러리의 tag/commit 소비 전략은 이후 변경 전에 명시적으로 확인한다
