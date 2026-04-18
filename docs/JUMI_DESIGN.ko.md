# JUMI Design

> 작성일: 2026-04-18
> 상태: 현재 기준 설계 문서
> 기준 코드베이스: `poc`, `dag-go`, `spawner`

---

## 1. 문서 목적

이 문서는 JUMI(Job Unit Management Interface)의 현재 설계 기준선을 고정한다.

JUMI는 Kubernetes 클러스터 내부에서 동작하는 policy-agnostic execution data-plane app이다.
JUMI의 역할은 정책적 선택을 수행하는 것이 아니라, 이미 실행 가능 형태로 정리된 executable run spec을 받아 실제 Kubernetes 실행으로 변환·제어·관찰하는 것이다.

이 문서의 목적은 아래와 같다.

- JUMI의 책임과 비책임을 고정한다.
- PoC에서 검증된 execution main path를 제품 초기 기준선으로 정리한다.
- executable run spec, registry, DAG execution, backend adapter, bounded release의 경계를 분리한다.
- Redis Streams, pipeline lowering, policy scheduling을 외부 계층으로 고정한다.
- JUMI core가 Kubernetes native baseline 위에서 성립해야 한다는 기준을 고정한다.
- JUMI가 Kueue-integrated target으로 개발되되, core execution semantics는 Kueue 부재에도 성립해야 한다는 기준을 고정한다.

---

## 2. JUMI 정의

JUMI = Job Unit Management Interface

이 이름은 다음 의미를 가진다.

- Job: Kubernetes 내부 실행 단위와 연결되는 의미
- Unit: run, node, attempt 같은 실행 단위를 포괄하는 의미
- Management: 상태, 실행, 취소, 실패 전파, 관찰을 포함한 제어 의미
- Interface: 상위 계층과 실행 계층 사이의 명확한 경계 의미

JUMI는 scheduler가 아니고, authored pipeline compiler도 아니다.
JUMI는 executable run spec을 받아 Kubernetes에서 run, node, attempt 단위를 실행·제어·관찰하는 execution core이자 data-plane app이다.

---

## 3. 범위

### 3.1 JUMI가 담당하는 것

- executable run spec 수신
- run 등록 및 상태 전이 관리
- DAG 실행 준비 및 ready node 판단 연결
- bounded release를 통한 실행 시작 속도 제어
- backend prepare/start/wait/cancel 호출
- fast-fail 및 cancel 전파
- run / node / attempt 상태 유지 및 노출
- 이벤트 및 병목 위치 기록
- in-cluster gRPC 기반 app-to-app 제어 인터페이스 제공

### 3.2 JUMI가 담당하지 않는 것

- authored pipeline spec 해석
- toolRef / binding / slot resolution
- fan-out lowering
- 정책 기반 우선순위 계산
- front-door queue 운영 정책
- Redis Streams consumer group 운영
- tenant fairness / retry class / backoff policy
- 장기 durable scheduler 기능

---

## 4. 배치 위치와 통신 원칙

### 4.1 배치 위치

JUMI는 Kubernetes 클러스터 내부에 배포되는 data-plane app이다.

의미:

- JUMI는 cluster-external authoring/UI 계층이 아니다.
- JUMI는 상위 policy scheduler를 대체하지 않는다.
- JUMI는 실행 요청을 실제 Kubernetes backend 동작으로 변환하는 in-cluster execution service다.

### 4.2 통신 원칙

- northbound는 executable run spec submit/status/cancel을 위한 gRPC app-to-app 인터페이스다.
- southbound는 internal DAG engine (dag-go), backend adapter, Kubernetes API와 연결된다.
- southbound에서 Kueue integration을 통해 admission visibility와 pending reason 관찰을 강화할 수 있다.
- 내부 service-to-service 통신은 gRPC를 우선한다.
- 내부 네트워크 기준선은 Cilium mesh 방식으로 둔다.
- HTTP는 health / readiness / status 같은 운영 endpoint에 우선 사용한다.

---

## 5. 전체 아키텍처

```text
Authoring / UI
  -> Pipeline Lowering Layer
  -> Executable Run Spec
  -> Policy Scheduler / Queue
  -> gRPC
  -> JUMI API
  -> JUMI Registry
  -> dag-go
  -> Backend Adapter
  -> Bounded Release Controller
  -> Kubernetes API
```

핵심 원칙은 아래와 같다.

- authored pipeline은 JUMI가 직접 해석하지 않는다.
- 정책 스케줄링은 JUMI 밖에서 결정한다.
- Redis Streams front-door queue는 JUMI 밖에 둔다.
- DAG readiness의 source of truth는 dag-go다.
- 실제 backend 실행 호출은 adapter/driver 계층이 담당한다.
- JUMI는 run lifecycle, node lifecycle, attempt lifecycle, failure propagation, bounded release, observability를 담당한다.
- JUMI core는 Kubernetes native baseline 위에서 성립해야 한다.
- JUMI는 Kueue-integrated target으로 개발되지만, Kueue 부재가 core failure의 원인이 되어서는 안 된다.

---

## 6. 구성요소

### 6.1 API Layer

주요 역할:

- executable run spec 수신
- 요청 기본 validation
- run 생성
- run 조회 API 제공
- cancel 요청 수신
- health / readiness / status endpoint 제공
- gRPC service endpoint 제공

비역할:

- authored pipeline parsing
- Redis Streams consumer group 운영
- policy scheduling 판단

### 6.2 Registry

주요 역할:

- run 상태 저장
- node 상태 저장
- attempt 상태 저장
- 이벤트 기록
- 현재 상태 스냅샷 제공

원칙:

- 현재 상태와 이벤트 히스토리를 섞지 않는다.
- 현재 상태는 조회 최적화용 스냅샷이다.
- 이벤트는 append-only 기록이다.

### 6.3 DAG Execution Layer

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

### 6.4 Backend Adapter

주요 역할:

- node spec -> backend 실행 요청 변환
- prepare / start / wait / cancel 호출
- backend 결과를 node/attempt 상태로 환원

초기 backend는 Kubernetes Job 기준이다.
JUMI는 이 backend 위에서 Kueue-integrated path를 함께 지원한다.

원칙:

- Kueue가 없다고 해서 prepare/start가 실패해서는 안 된다.
- Kueue와 연동될 때는 admission/queue 관찰을 강화할 수 있어야 한다.
- Kueue 관련 필드가 없어도 Kubernetes native 실행은 가능해야 한다.

### 6.5 Bounded Release Controller

주요 역할:

- 동시에 시작 가능한 node 수 제한
- Kubernetes API burst 억제
- 대량 fan-out 상황에서 release pacing 적용

비역할:

- 우선순위 결정
- queue selection
- policy scheduling

### 6.6 Observer / Event Layer

주요 역할:

- run / node / attempt 이벤트 기록
- current bottleneck location 분류
- terminal stop cause 기록
- terminal failure reason 기록
- 상태 변화 추적

원칙:

- current execution status, current bottleneck location, terminal stop cause, terminal failure reason을 분리한다.
- 하나의 필드에 모든 의미를 밀어 넣지 않는다.
- Kueue가 없는 배포에서도 상태 모델은 완결적으로 유지되어야 한다.
- Kueue-integrated path에서는 admission visibility와 pending reason을 추가로 관찰할 수 있어야 한다.

---

## 7. 입력 계약

JUMI의 입력은 authored pipeline이 아니라 Executable Run Spec이다.

### 7.1 입력 원칙

- authored 의미 해석은 JUMI 밖에서 끝나야 한다.
- pipeline lowering이 끝난 executable run spec만 JUMI에 들어와야 한다.
- JUMI는 실행과 관찰에 필요한 정보만 직접 이해한다.
- Kueue 전용 입력이 없어도 spec은 유효할 수 있어야 한다.

### 7.2 최소 필드

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

### 7.3 validation 기준

- runId는 요청 단위에서 유일해야 한다.
- nodeId는 run 내부에서 유일해야 한다.
- 모든 edge endpoint는 존재하는 node를 가리켜야 한다.
- graph는 DAG여야 한다.
- retryPolicy가 없으면 no-retry로 해석한다.
- failurePolicy가 없으면 fail-fast를 기본값으로 둔다.

---

## 8. 상태 모델 방향

상태 전이의 상세 규칙은 별도 문서에서 정의한다.
여기서는 방향만 고정한다.

- run 상태와 node 상태를 분리한다.
- node 상태와 attempt 상태를 분리한다.
- 상태는 일관된 전이 규칙 아래 유지하고 API로 노출한다.
- 현재 병목 위치와 최종 종료 원인을 같은 필드로 섞지 않는다.

상세 기준 문서:

- `JUMI_STATE_TRANSITION_SPEC.ko.md`

---

## 9. PoC 기준 매핑

이 설계는 현재 PoC의 main path를 직접 기반으로 한다.

| JUMI 구성요소 | 현재 PoC 대응 |
|--------------|---------------|
| API Layer | 현재는 `cmd/*` 진입점이 대체 역할 |
| Registry | `pkg/store` |
| Ingress registration | `pkg/ingress/run_gate.go` |
| DAG Execution Layer | `dag-go` + 각 `cmd/*` DAG wiring |
| Backend Adapter | `pkg/adapter/spawner_node.go` |
| Bounded Release Controller | `spawner`의 `BoundedDriver` 계층 |
| Observer / stop cause | `pkg/observe` |

초기 제품화 원칙:

- `pkg/session` 실험 경로는 main path 기준선으로 삼지 않는다.
- JUMI의 기준선은 `RunGate -> dag-go -> SpawnerNode -> BoundedDriver -> K8s` 경로다.
- Redis Streams 경로는 JUMI 내부 main path로 들이지 않는다.
- baseline path와 Kueue-integrated path를 함께 설계 대상으로 본다.

---

## 10. 외부 계층과의 경계

### 10.1 Redis Streams

Redis Streams 기반 queue는 향후 policy scheduler 또는 front-door ingress 계층의 구현 선택지다.
JUMI 내부 핵심 구성요소가 아니다.

### 10.2 Pipeline Lowering

pipeline authored model과 lowering은 외부 계층이다.
JUMI는 executable run spec만 계약 대상으로 본다.

### 10.3 Kueue

Kueue는 JUMI의 필수 전제조건이 아니다.
JUMI core는 Kubernetes native baseline 위에서 완결적으로 동작해야 한다.

원칙:

- JUMI core execution은 Kubernetes native backend 위에서 성립해야 한다.
- JUMI는 Kueue-integrated target을 지향하며, Kueue integration은 admission visibility, pending reason, cluster policy alignment를 강화하는 통합 계층이다.
- Kueue 관련 기능이 비활성화되거나 설치되지 않아도 run submit/start/wait/cancel은 정상이여야 한다.
- Kueue 관련 오류는 integration failure로 다뤄야 하며, core backend failure와 구분해야 한다.

---

## 11. 테스트 기준

### 11.1 단위 테스트

- run validation
- run/node/attempt 상태 전이
- failure propagation
- cancel propagation
- bounded release 동작
- stop cause 분류
- no-Kueue backend path 검증

### 11.2 통합 테스트

- A -> B -> C
- fan-out / collect
- fast-fail
- mixed duration nodes
- bounded release saturation
- cancel during running
- gRPC submit/status/cancel roundtrip
- Kueue-integrated path 검증

### 11.3 e2e 테스트

- kind 또는 테스트 클러스터 기준 실행
- Job/Pod 생성 검증
- 실행 완료/실패 전파 검증
- Kubernetes native baseline 경로 검증
- Kueue admission pending 관찰 검증
- in-cluster gRPC 통신 검증

---

## 12. 한 줄 요약

JUMI는 executable run spec을 받아 Kubernetes 위에서 run, node, attempt 단위를 실행·제어·관찰하는 in-cluster policy-agnostic execution data-plane app이며, Kueue와 연동되지만 core execution semantics는 Kueue에 종속되지 않는다.
