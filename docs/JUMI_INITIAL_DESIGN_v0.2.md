# JUMI Initial Design v0.2

> 작성일: 2026-04-18
> 상태: 초기 설계 기준선
> 기준 코드베이스: `pipeline-lite-poc`

---

## 1. 범위 / 용어

### 1.1 문서 목적

이 문서는 JUMI(Job Unit Management Interface)의 초기 제품 설계 기준선을 고정하기 위한 문서다.

JUMI는 Kubernetes 클러스터 내부에서 동작하는 policy-agnostic execution app이다.
JUMI의 목적은 authored pipeline을 해석하거나 정책 스케줄링을 수행하는 것이 아니라,
이미 실행 가능한 형태로 정리된 실행 명세를 받아 DAG 기반 실행을 안정적으로 수행하는 것이다.

이 문서의 목적은 다음과 같다.

- JUMI의 책임과 비책임을 고정한다.
- PoC에서 검증된 execution main path를 제품 초기 기준선으로 정리한다.
- executable run spec, registry, bounded release, backend adapter의 경계를 분리한다.
- 상태 모델, cancel semantics, retry semantics, observability semantics를 명시한다.
- 이후 구현/리팩터링 시 execution app의 책임이 scheduler/compiler 쪽으로 번지는 것을 막는다.

### 1.2 JUMI 정의

JUMI = Job Unit Management Interface

이 이름은 다음 의미를 가진다.

- Job: Kubernetes 내부 실행 단위와 연결되는 의미
- Unit: run, node, attempt 같은 실행 단위를 포괄하는 의미
- Management: 상태, 실행, 취소, 실패 전파, 관찰을 포함한 제어 의미
- Interface: 상위 계층과 실행 계층 사이의 명확한 경계 의미

JUMI는 scheduler가 아니고, authored pipeline compiler도 아니다.
JUMI는 executable run spec을 받아 Kubernetes에서 실행/관찰/제어하는 실행 코어다.

### 1.3 범위

JUMI가 담당하는 범위는 아래와 같다.

- executable run spec 수신
- run 등록 및 상태 전이 관리
- DAG 실행 준비 및 ready node 판단 연결
- bounded release를 통한 실행 시작 속도 제어
- backend prepare/start/wait/cancel 호출
- fast-fail 및 cancel 전파
- run / node / attempt 상태 노출
- 이벤트 및 병목 원인 기록

JUMI가 현재 범위에 포함하지 않는 것은 아래와 같다.

- authored pipeline spec 해석
- toolRef / binding / slot resolution
- fan-out lowering
- 정책 기반 우선순위 계산
- front-door queue 운영 정책
- tenant fairness / retry class / backoff class 정책
- 장기 durable scheduler 기능

### 1.4 용어

- Run: 하나의 실행 요청 단위
- Node: DAG 내의 실행 단위
- Attempt: Node 실행의 개별 시도
- Executable Run Spec: JUMI가 직접 이해하고 실행할 수 있는 입력 명세
- Policy Scheduler: admission, priority, retry class 같은 정책을 결정하는 외부 계층
- Pipeline Lowering Layer: authored pipeline을 executable run spec으로 변환하는 외부 계층
- Bounded Release: ready node를 한 번에 무제한 시작하지 않고 일정한 속도/개수로 릴리즈하는 방식
- Registry: run / node / attempt / event 상태를 기록하고 조회하는 계층
- Stop Cause: 운영자 관점에서 현재 병목 또는 정지 원인을 설명하는 분류

---

## 2. 전체 아키텍처

JUMI는 플랫폼의 중심이 아니라 execution kernel로 동작한다.

전체 흐름은 아래처럼 본다.

```text
Authoring / UI
  -> Pipeline Lowering Layer
  -> Executable Run Spec
  -> Policy Scheduler / Queue
  -> JUMI API
  -> JUMI Registry
  -> dag-go
  -> Spawner Adapter
  -> Bounded Driver
  -> Kubernetes API
```

핵심 원칙은 다음과 같다.

- authored pipeline은 JUMI가 직접 해석하지 않는다.
- 정책 스케줄링은 JUMI 밖에서 결정한다.
- DAG readiness의 source of truth는 dag-go다.
- 실제 backend 실행 호출은 spawner/driver 계층이 담당한다.
- JUMI는 run lifecycle, node lifecycle, attempt lifecycle, failure propagation, bounded release, observability를 담당한다.

즉 JUMI는 "무엇을 우선 실행할지"를 결정하지 않고,
"이미 실행하기로 결정된 run을 안전하게 실행하는 것"에 집중한다.

---

## 3. 구성요소

### 3.1 API Layer

외부로부터 실행 요청을 받는 계층이다.

주요 역할:

- executable run spec 수신
- 요청 기본 validation
- run 생성
- run 조회 API 제공
- cancel 요청 수신
- health / readiness / status endpoint 제공

비역할:

- authored pipeline parsing
- Redis consumer group 운영
- policy scheduling 판단

### 3.2 Registry

run과 node/attempt 상태를 저장하고 조회하는 계층이다.

주요 역할:

- run 상태 저장
- node 상태 저장
- attempt 상태 저장
- 이벤트 기록
- 현재 상태 스냅샷 제공
- stop cause 계산에 필요한 최소 상태 제공

원칙:

- 현재 상태와 이벤트 히스토리를 같은 개념으로 섞지 않는다.
- 현재 상태는 조회 최적화용 스냅샷이다.
- 이벤트는 append-only 기록이다.
- 상태 전이는 이벤트와 함께 기록되지만, 조회 API는 현재 상태를 직접 반환한다.

초기 구현 후보:

- in-memory registry
- JSON/file 기반 durable-lite registry
- 외부 저장소와 결합한 경량 registry

### 3.3 DAG Execution Layer

JUMI 내부의 실행 중심 계층이다.

주요 역할:

- executable run spec을 DAG 구조로 연결
- ready node 판단을 dag-go에 위임
- dependency 완료 여부 연결
- fast-fail 전파
- cancel 전파

원칙:

- readiness 계산을 JUMI 내부에서 별도로 다시 구현하지 않는다.
- dag-go를 실행 판단의 기준선으로 둔다.
- JUMI는 dag-go 위에 얹힌 orchestration boundary로 남는다.

### 3.4 Spawner Adapter

DAG node를 실제 backend 실행 요청으로 변환하는 계층이다.

주요 역할:

- node spec -> backend 실행 요청 변환
- prepare / start / wait / cancel 호출
- backend 결과를 node/attempt 상태로 환원

초기 backend는 Kubernetes 기준이다.
향후 다른 backend가 생기더라도 JUMI core 변경은 최소화해야 한다.

### 3.5 Bounded Release Controller

ready node를 한 번에 과도하게 릴리즈하지 않도록 제어하는 계층이다.

주요 역할:

- 동시 start 가능 node 수 제한
- Kubernetes API burst 억제
- 대량 fan-out 상황에서 release pacing 적용

비역할:

- 우선순위 결정
- queue selection
- policy scheduling

이 계층은 scheduler가 아니다.
이미 ready인 node를 얼마나 빠르게 시작할지 조절할 뿐이다.

### 3.6 Observer / Event Layer

운영자가 병목 지점과 실패 원인을 구분할 수 있도록 돕는 계층이다.

주요 역할:

- run / node / attempt 이벤트 기록
- 현재 병목 위치 분류
- terminal failure reason 기록
- 상태 변화 추적
- 관찰 지표 제공

초기 stop cause 예시는 아래와 같다.

- accepted_wait
- release_wait
- backend_prepare
- backend_start
- kueue_pending
- scheduler_pending
- running
- failed
- canceled
- finished

원칙:

- current status, current stop cause, terminal failure reason을 분리한다.
- 하나의 필드에 모든 의미를 밀어 넣지 않는다.

---

## 4. 입력 계약

JUMI는 authored pipeline 전체를 입력으로 받지 않는다.
JUMI의 입력은 Executable Run Spec이다.

### 4.1 입력 원칙

- authored 의미 해석은 JUMI 밖에서 끝나야 한다.
- JUMI는 "이 명세는 실행 가능하다"는 전제 하에 동작한다.
- slot resolution, toolRef expansion, fan-out lowering은 외부 계층 책임이다.
- JUMI는 실행과 관찰에 필요한 정보만 직접 이해한다.

### 4.2 최소 필드

Executable Run Spec에는 최소한 아래 정보가 필요하다.

Run 공통:

- runId
- graph
- failurePolicy
- metadata
- submittedAt 또는 등가 정보

Graph:

- nodes
- edges

Node:

- nodeId
- image
- command
- args
- env
- executionClass
- resourceProfile
- timeoutPolicy
- retryPolicy 또는 retry 없음 명시
- mounts
- inputs
- outputs

### 4.3 추가 계약

초기 버전에서는 아래 규칙을 validation으로 강제한다.

- runId는 요청 단위에서 유일해야 한다.
- nodeId는 run 내부에서 유일해야 한다.
- 모든 edge endpoint는 존재하는 node를 가리켜야 한다.
- graph는 DAG여야 한다.
- executionClass는 허용된 값이어야 한다.
- timeoutPolicy 단위는 명확해야 한다.
- retryPolicy가 없으면 no-retry로 해석한다.
- failurePolicy가 없으면 fail-fast를 기본값으로 둔다.

### 4.4 JSON 예시

```json
{
  "runId": "run-20260418-001",
  "submittedAt": "2026-04-18T09:00:00Z",
  "failurePolicy": {
    "mode": "fail-fast"
  },
  "metadata": {
    "pipelineName": "fanout-collect",
    "submitter": "scheduler-a"
  },
  "graph": {
    "nodes": [
      {
        "nodeId": "a",
        "image": "busybox:1.36",
        "command": ["sh", "-c"],
        "args": ["echo prepare > /work/a.txt"],
        "executionClass": "standard",
        "resourceProfile": {
          "cpu": "100m",
          "memory": "128Mi"
        },
        "timeoutPolicy": {
          "seconds": 300
        },
        "retryPolicy": {
          "maxAttempts": 1
        },
        "mounts": [],
        "inputs": [],
        "outputs": ["a.txt"]
      },
      {
        "nodeId": "b",
        "image": "busybox:1.36",
        "command": ["sh", "-c"],
        "args": ["cat /work/a.txt > /out/b.txt"],
        "executionClass": "standard",
        "resourceProfile": {
          "cpu": "100m",
          "memory": "128Mi"
        },
        "timeoutPolicy": {
          "seconds": 300
        },
        "retryPolicy": {
          "maxAttempts": 1
        },
        "mounts": [],
        "inputs": ["a.txt"],
        "outputs": ["b.txt"]
      }
    ],
    "edges": [
      ["a", "b"]
    ]
  }
}
```

---

## 5. 상태 모델

### 5.1 설계 원칙

- run 상태와 node 상태를 분리한다.
- node 상태와 attempt 상태를 분리한다.
- 현재 상태와 이벤트 히스토리를 섞지 않는다.
- 운영자가 "어디에서 멈췄는지"를 읽을 수 있어야 한다.
- 상태 이름은 가능한 한 JUMI 내부 의미를 반영한다.

### 5.2 Run 상태

Run 수준 상태는 아래를 기준으로 둔다.

- Accepted
- Dispatching
- Running
- Succeeded
- Failed
- Canceled

정의:

- Accepted: API가 spec validation과 run 등록을 마쳤다.
- Dispatching: DAG wiring 및 node release가 시작되었으나 terminal이 아니다.
- Running: 적어도 하나의 node attempt가 backend 실행 중이다.
- Succeeded: 모든 required node가 성공했다.
- Failed: fast-fail 또는 retry exhaustion 등으로 run이 실패했다.
- Canceled: 사용자 또는 시스템 취소가 terminal로 반영되었다.

### 5.3 Node 상태

Node 수준 상태는 아래를 기준으로 둔다.

- Pending
- Ready
- Releasing
- Starting
- Running
- Succeeded
- Failed
- Canceled
- Skipped

정의:

- Pending: dependency가 아직 충족되지 않았다.
- Ready: dependency가 충족되어 release 대상이 되었다.
- Releasing: bounded release에 의해 start 슬롯을 기다린다.
- Starting: backend prepare/start 수행 중이다.
- Running: backend 실행이 시작되었다.
- Succeeded: terminal success
- Failed: terminal failure
- Canceled: terminal canceled
- Skipped: upstream failure policy로 더 이상 실행되지 않는다.

### 5.4 Attempt 상태

Attempt 수준 상태는 아래를 기준으로 둔다.

- Prepared
- Started
- Completed
- Errored
- Canceled

정의:

- Prepared: backend prepare 성공
- Started: backend start 성공
- Completed: backend wait가 terminal result를 반환
- Errored: prepare/start/wait 중 하나가 실패
- Canceled: cancel 요청이 backend에 전달되었거나 cancel terminal로 종료

### 5.5 상태 전이 표

Run 전이:

```text
Accepted -> Dispatching -> Running -> Succeeded
Accepted -> Dispatching -> Running -> Failed
Accepted -> Dispatching -> Running -> Canceled
Accepted -> Canceled
Dispatching -> Failed
Dispatching -> Canceled
```

Node 전이:

```text
Pending -> Ready -> Releasing -> Starting -> Running -> Succeeded
Pending -> Skipped
Ready -> Canceled
Releasing -> Canceled
Starting -> Failed
Starting -> Canceled
Running -> Failed
Running -> Canceled
```

Attempt 전이:

```text
Prepared -> Started -> Completed
Prepared -> Errored
Started -> Errored
Started -> Canceled
```

### 5.6 retry와 상태 관계

retry는 attempt 수준에서 일어난다.

- 하나의 node는 여러 attempt를 가질 수 있다.
- attempt가 `Errored` 또는 실패 terminal이면 retry policy에 따라 새 attempt를 만들 수 있다.
- retry 가능한 경우 node 상태는 `Failed` terminal로 닫지 않고 `Ready` 또는 `Releasing`으로 되돌아간다.
- retry exhaustion이 발생하면 그때 node를 `Failed`로 terminal 처리한다.

---

## 6. 실행 의미론

### 6.1 readiness source of truth

JUMI는 dependency readiness를 별도 엔진으로 다시 계산하지 않는다.
ready node 판별의 기준은 dag-go다.

JUMI는 다음 책임만 가진다.

- dag-go가 ready로 판단한 node를 release 대상으로 받는다.
- release pacing을 적용한다.
- backend 실행과 상태 기록을 연결한다.

### 6.2 bounded release semantics

bounded release는 ready node 집합에 대해 start 속도를 제한한다.

초기 규칙:

- max concurrent release를 설정값으로 둔다.
- ready node는 FIFO 또는 구현 정의 순서로 release할 수 있다.
- bounded release는 fairness나 priority를 보장하지 않는다.
- bounded release saturation은 stop cause `release_wait`로 관찰 가능해야 한다.

### 6.3 fast-fail semantics

기본 failure policy는 fail-fast다.

초기 규칙:

- 어떤 node가 terminal failure가 되면 새로운 downstream node는 시작하지 않는다.
- 아직 시작되지 않은 downstream node는 `Skipped`로 마감한다.
- 이미 실행 중인 sibling은 즉시 죽어야 하는 것은 아니다.
- policy가 허용하면 이미 실행 중인 sibling에 대해 best-effort cancel을 보낼 수 있다.

### 6.4 cancel semantics

cancel은 "더 이상 새 작업을 시작하지 않음"과 "이미 시작된 작업에 cancel을 시도함"을 의미한다.

초기 규칙:

- cancel 요청이 수락되면 run은 canceling 의미의 내부 흐름으로 들어간다.
- 아직 시작되지 않은 node는 시작하지 않는다.
- `Pending`, `Ready`, `Releasing` node는 `Canceled` 또는 `Skipped`로 terminal 처리한다.
- `Starting`, `Running` node에는 backend cancel을 best-effort로 시도한다.
- backend cancel 응답이 늦거나 completion과 경쟁하는 것은 허용한다.
- 이미 성공한 node는 되돌리지 않는다.
- run terminal은 모든 in-flight attempt가 terminal이 되거나, 시스템이 cancel completion 기준을 만족했을 때 `Canceled`로 기록한다.

### 6.5 retry semantics

retry는 node별로 명시된 retry policy에 따라 동작한다.

초기 규칙:

- 기본값은 no-retry다.
- retry policy가 있으면 node 단위로 적용한다.
- prepare error, start error, wait error를 동일한 retry bucket으로 볼지 여부는 정책 필드로 분리 가능하지만, v0.2에서는 동일 bucket으로 본다.
- retry 시 새 attempt ID를 발급한다.
- retry exhaustion이 발생하면 node를 `Failed`로 terminal 처리한다.
- fail-fast policy와 retry policy가 충돌하면 retry exhaustion 이후 fail-fast를 적용한다.

---

## 7. API

### 7.1 Run 제출 API

`POST /runs`

역할:

- executable run spec 제출
- 기본 validation 수행
- run 등록
- accepted 응답 반환

의미:

- 이 API는 등록 성공을 의미한다.
- 실행 완료를 기다리는 동기 API가 아니다.
- accepted 응답 이후 dispatch/execution은 비동기 진행이 기본이다.

응답 예시:

- runId
- status
- acceptedAt

### 7.2 Run 상태 읽기 API

`GET /runs/{runId}`

역할:

- run 현재 상태 반환
- run 요약 정보 제공
- 현재 stop cause 제공 가능
- terminal failure reason 제공 가능

### 7.3 Node 상태 읽기 API

`GET /runs/{runId}/nodes`

역할:

- node별 상태 제공
- node별 시작/종료 시간 제공
- node별 failure reason / attempt 수 제공
- current stop cause 제공 가능

### 7.4 Cancel API

`POST /runs/{runId}:cancel`

역할:

- run 취소 요청
- 아직 시작되지 않은 downstream node 중단
- 이미 시작된 node에 cancel 전파 시도

응답 의미:

- cancel request accepted 여부를 반환한다.
- 실제 backend cancel 완료까지 동기 대기하지 않는다.

---

## 8. Health / Status API

### 8.1 Liveness

`GET /healthz`

의미:

- 프로세스가 살아있는지 확인

### 8.2 Readiness

`GET /readyz`

의미:

- 새 run을 받아도 되는 상태인지 확인

ready 판단 예시:

- registry 초기화 완료
- backend adapter 준비 완료
- bounded release controller 준비 완료
- 내부 오류 상태가 치명적이지 않음

### 8.3 Status

`GET /statusz`

의미:

- 현재 실행 중 run 수
- release 대기 node 수
- backend 연결 상태
- 최근 오류 요약

---

## 9. 관찰 모델

### 9.1 분리 원칙

초기 API와 이벤트는 아래 세 가지를 분리한다.

- current status
- current stop cause
- terminal failure reason

예시:

- current status = `Dispatching`
- current stop cause = `release_wait`
- terminal failure reason = 없음

다른 예시:

- current status = `Failed`
- current stop cause = 없음
- terminal failure reason = `backend_start_error`

### 9.2 stop cause 예시

초기 stop cause 분류:

- accepted_wait
- release_wait
- backend_prepare
- backend_start
- kueue_pending
- scheduler_pending
- running
- finished

### 9.3 failure reason 예시

terminal failure reason 분류:

- validation_error
- backend_prepare_error
- backend_start_error
- backend_wait_error
- retry_exhausted
- dependency_failed
- cancellation_propagated
- internal_error

### 9.4 이벤트 예시

- run accepted
- run dispatching
- node ready
- node release delayed
- attempt prepared
- attempt started
- attempt completed
- attempt errored
- node failed
- run canceled

---

## 10. 테스트

### 10.1 단위 테스트

- run validation
- run/node/attempt 상태 전이
- failure propagation
- cancel propagation
- bounded release 동작
- stop cause 분류
- retry exhaustion

### 10.2 통합 테스트

- A -> B -> C
- fan-out / collect
- fast-fail
- mixed duration nodes
- bounded release saturation
- cancel during running

### 10.3 e2e 테스트

- kind 또는 테스트 클러스터 기준 실행
- Job/Pod 생성 검증
- 실행 완료/실패 전파 검증
- Kueue pending / scheduler pending 구분 검증

---

## 11. 샘플

초기 샘플은 authored pipeline이 아니라 executable run spec fixture 중심으로 유지한다.

추천 샘플:

- simple-abc
- fanout-collect
- fastfail
- mixed-duration
- bounded-release
- cancel-running

이 샘플들의 목적은 spec richness 검증이 아니라 execution kernel 안정성 검증이다.

---

## 12. 구성 / 경로 정책

### 12.1 구성 원칙

- 환경 의존 설정은 app config에 둔다.
- 실행 의미를 바꾸는 설정은 request spec에 둔다.
- authored-level 의미는 외부 계층에서 정리하고 들어온다.

### 12.2 예시 구성

- max concurrent release
- backend type
- namespace default
- event retention
- graceful shutdown timeout
- metrics enablement

### 12.3 경로 정책

초기 기준선은 아래처럼 둔다.

- `/in`: read-only
- `/work`: read-write
- `/out`: read-write

향후 heavy-path bind 최적화가 들어가더라도 외부 계약은 유지해야 한다.

---

## 13. 운영 체크리스트

- readiness false일 때 새 run을 막는가
- cancel 요청이 stuck되지 않는가
- bounded release 값이 burst를 억제하는가
- run 상태와 node 상태가 서로 모순되지 않는가
- failure reason이 API에 반영되는가
- current status, stop cause, failure reason이 분리되어 있는가
- graceful shutdown 시 실행 중 run 처리 정책이 있는가

---

## 14. 현재 PoC 코드와의 매핑

이 설계는 현재 PoC의 main path를 직접 기반으로 한다.

| JUMI 구성요소 | 현재 PoC 대응 |
|--------------|---------------|
| API Layer | 현재는 `cmd/*` 진입점이 대체 역할 |
| Registry | `pkg/store` |
| Ingress registration | `pkg/ingress/run_gate.go` |
| DAG Execution Layer | `dag-go` + 각 `cmd/*` DAG wiring |
| Spawner Adapter | `pkg/adapter/spawner_node.go` |
| Bounded Release Controller | `spawner/cmd/imp.BoundedDriver`를 각 `cmd/*`에서 wrapping |
| Observer / stop cause | `pkg/observe` |
| Experimental path | `pkg/session` |

초기 제품화 원칙:

- `pkg/session` 실험 경로는 main path 기준선으로 삼지 않는다.
- JUMI의 기준선은 `RunGate -> dag-go -> SpawnerNode -> BoundedDriver -> K8s` 경로다.

---

## 15. 한계 및 로드맵

### 15.1 현재 한계

- authored pipeline 직접 해석 없음
- policy scheduler 없음
- Redis Streams ingress는 외부 계층으로 분리 예정
- durable recovery 미완성
- tenant fairness / priority / retry class 미구현

### 15.2 로드맵

Phase 1:

- JUMI core 골격 고정
- executable run spec 경계 확정
- run/node/attempt 상태 모델 고정
- bounded release 안정화

Phase 2:

- status/event/metrics 확장
- cancel / fast-fail semantics 강화
- registry 개선

Phase 3:

- external scheduler adapter 연결
- Redis Streams front-door 연결
- pipeline lowering layer 연결

Phase 4:

- recovery / replay / idempotency 강화
- operator/control plane 연계
- 운영 지표 및 추적 고도화

---

## 16. 빠른 시작

초기 구현 우선순위는 아래 순서가 좋다.

1. `POST /runs`
2. in-memory registry
3. dag-go wiring
4. spawner adapter 연결
5. bounded release controller 연결
6. `GET /runs/{id}`
7. `POST /runs/{id}:cancel`
8. health / readiness / status endpoint
9. executable spec fixture 기반 통합 테스트
10. kind e2e

---

## 17. 설계 원칙 요약

- JUMI는 policy scheduler가 아니다.
- JUMI는 authored pipeline compiler가 아니다.
- JUMI는 executable run spec 기반 execution app이다.
- DAG readiness 판단은 dag-go를 기준으로 한다.
- backend execution은 spawner/driver 계층에 위임한다.
- bounded release는 scheduler가 아니라 pacing controller다.
- 정책 스케줄링과 pipeline lowering은 외부 계층에 남긴다.
- current status, stop cause, failure reason을 분리한다.

---

## 18. JUMI 한 줄 설명

JUMI is a policy-agnostic execution interface for managing run, node, and attempt units on Kubernetes.

한국어로는 다음과 같이 정리할 수 있다.

JUMI는 Kubernetes 위에서 run, node, attempt 단위를 실행·제어·관찰하는 policy-agnostic execution interface다.