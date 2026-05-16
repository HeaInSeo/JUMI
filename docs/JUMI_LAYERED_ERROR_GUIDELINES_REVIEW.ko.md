# JUMI Layered Error Guidelines Review

> 작성일: 2026-05-16
> 목적: JUMI, AH, dag-go, spawner, Kubernetes, Kueue를 포함한 계층별 에러 의미론 초안을 리뷰용으로 고정한다.
> 상태: 리뷰 초안

---

## 1. 왜 이 문서가 필요한가

현재 JUMI는 단순 DAG executor를 넘어 다음 계층과 연결된다.

- northbound API
- JUMI core state machine
- dag-go
- artifact-handoff
- spawner
- Kubernetes / Kueue

이 구조에서는 "에러가 발생했다"는 사실보다 아래가 더 중요하다.

- 어느 계층에서 발생했는가
- 어느 계층이 의미를 최종 소유하는가
- northbound에 어떤 failure reason으로 노출할 것인가
- retry 가능 여부를 어디서 판단할 것인가

이 문서의 핵심 원칙은 다음 한 줄이다.

JUMI는 raw library/driver error를 그대로 외부에 노출하지 않고, run/node/attempt 의미론으로 정규화해서 노출해야 한다.

---

## 2. 최상위 원칙

### 2.1 의미 소유권

- `dag-go`는 graph/readiness/execution primitive 계층이다.
- `spawner`는 Kubernetes execution driver 계층이다.
- `artifact-handoff`는 execution-time artifact seam 계층이다.
- Kubernetes / Kueue는 외부 시스템이다.
- JUMI는 위 계층들의 raw 결과를 run/node/attempt 상태와 이벤트로 환원하는 의미 소유 계층이다.

### 2.2 분리 원칙

분리해야 하는 것은 아래다.

- raw error origin
- JUMI internal classification
- node terminal failure reason
- run terminal failure reason
- retryability
- observability event

즉 하나의 에러 문자열로 모든 것을 설명하려 하면 안 된다.

### 2.3 하위호환성 원칙

- 하위 라이브러리의 에러 표현이 바뀌어도 JUMI northbound failure taxonomy는 가능한 한 유지한다.
- raw dependency error shape는 변할 수 있어도, JUMI가 외부에 약속하는 failure reason은 더 천천히 바꾼다.
- 하위호환성이 깨지는 변경은 JUMI 내부 매핑 레이어에서 흡수하는 것을 우선한다.

---

## 3. 계층별 에러 분류 초안

### 3.1 Northbound API error

예:

- invalid run spec
- duplicate run id
- missing run id / node id
- query target not found

특징:

- submit/query/cancel 호출 시점에 즉시 반환
- execution lifecycle에 진입하기 전 계층

JUMI 책임:

- validation error와 execution error를 섞지 않는다
- duplicate submit과 internal submit failure를 구분한다

### 3.2 JUMI core semantic error

예:

- invalid state transition
- cancel race 정리 실패
- run/node/attempt 모델 불일치
- registry update failure

특징:

- JUMI 내부 의미론이 깨진 상태
- dependency raw error보다 심각한 계층

JUMI 책임:

- 가능한 한 dependency error와 분리된 failure reason으로 노출
- retry보다 먼저 internal bug / invariant break 관점으로 본다

### 3.3 dag-go error

예:

- invalid DAG
- runner wiring failure
- execution orchestration misuse
- dependency graph invariant violation

특징:

- pure graph/execution primitive 계층
- 운영 환경보다 의미론과 불변식이 중요

JUMI 책임:

- dag-go raw error를 그대로 northbound contract로 삼지 않는다
- graph/input 문제인지 runtime orchestration 문제인지 분류한다

### 3.4 AH seam error

예:

- binding resolve failure
- artifact register failure
- node terminal notify failure
- sample finalize failure
- GC evaluate failure
- response decode drift

특징:

- execution-time artifact seam
- artifact-aware execution semantics에 직접 영향

JUMI 책임:

- resolve 실패와 artifact missing을 구분한다
- register/finalize/GC 실패가 node failure인지 run post-processing failure인지 구분한다
- contract drift는 integration error로 식별 가능해야 한다

### 3.5 spawner / Kubernetes driver error

예:

- prepare failure
- start failure
- wait failure
- cancel propagation failure
- output metadata collection failure
- runtime helper integration failure

특징:

- pure library보다 driver 성격이 강함
- Kubernetes API, Job, Pod, container runtime 상태에 민감

JUMI 책임:

- `backend_prepare_error`, `backend_start_error`, `backend_wait_error` 수준에서 끝내지 말고 세분화 여지를 남긴다
- spec translation error와 runtime environment error를 분리한다

### 3.6 Kubernetes / Kueue observation error

예:

- Kueue pending
- unschedulable pod
- admission observation unavailable
- workload/pod state ambiguity

특징:

- 항상 terminal failure는 아닐 수 있음
- 관찰 신호와 core execution failure를 구분해야 함

JUMI 책임:

- 관찰 실패를 execution failure와 혼동하지 않는다
- pending / admitted / scheduled / unschedulable을 상태 모델에 분리 반영한다

---

## 4. JUMI가 최종적으로 정규화해야 하는 축

각 raw error는 최종적으로 아래 축으로 정규화되는 것이 바람직하다.

- `origin_layer`
- `failure_reason`
- `stop_cause`
- `retryable`
- `event_type`
- `node_status_transition`
- `run_status_impact`

예:

- `origin_layer=spawner`
- `failure_reason=backend_start_error`
- `stop_cause=failed`
- `retryable=false`
- `event_type=node.failed`

이 축을 명시적으로 정리하지 않으면 하위 라이브러리 변화가 곧 northbound contract drift가 된다.

---

## 5. 리뷰 포인트

이 문서를 리뷰할 때 특히 확인할 질문은 아래다.

1. JUMI가 의미 소유 계층이라는 전제에 동의하는가
2. dag-go raw error를 JUMI가 semantic error로 다시 매핑하는 구조가 맞는가
3. spawner를 generic backend보다 K8s driver로 보는 관점에 동의하는가
4. AH failure를 execution failure와 post-processing failure로 분리해야 하는가
5. Kueue 관련 상태를 failure taxonomy 밖의 observation 계층으로 충분히 분리하고 있는가

---

## 6. 후속 작업 제안

이 문서가 합의되면 다음 작업 순서는 아래가 적절하다.

1. dag-go integration contract 문서 고정
2. spawner integration contract 문서 고정
3. 현재 JUMI 코드의 `failureReason` / `event type` / `retryability` taxonomy 점검
4. contract test 또는 compatibility test 도입 범위 결정
