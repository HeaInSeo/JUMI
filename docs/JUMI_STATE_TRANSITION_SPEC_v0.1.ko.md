# JUMI State Transition Spec v0.1

> 작성일: 2026-04-18
> 목적: JUMI의 run, node, attempt 상태 전이 규칙을 초기 기준선으로 고정한다.

---

## 1. 문서 목적

이 문서는 JUMI가 유지해야 하는 상태 모델을 전이 규칙 중심으로 정의한다.

이 문서의 목적은 아래와 같다.

- run, node, attempt 상태를 서로 분리한다.
- 상태 이름보다 전이 규칙을 먼저 고정한다.
- API 노출 상태와 내부 실행 이벤트의 관계를 명확히 한다.
- cancel, failure, retry semantics 문서의 기반을 만든다.

이 문서는 retry/backoff/classification의 복잡한 정책을 정의하지 않는다.
그 부분은 외부 계층 책임이며, JUMI는 제한된 execution semantics만 다룬다.

---

## 2. 상태 모델 원칙

- run 상태와 node 상태는 분리한다.
- node 상태와 attempt 상태는 분리한다.
- 현재 상태와 이벤트 히스토리는 분리한다.
- 모든 상태는 허용된 전이 규칙 아래에서만 변경된다.
- 상태는 API로 노출되기 전에 내부적으로 일관성을 유지해야 한다.
- dag-go readiness는 node 실행 가능성 판단의 기준선이지만, JUMI 상태 모델 전체를 대체하지는 않는다.

---

## 3. Run State

### 3.1 상태 목록

- Accepted
- Admitted
- Running
- Succeeded
- Failed
- Canceled

### 3.2 상태 의미

- Accepted: run spec이 기본 검증을 통과하고 registry에 등록되었다.
- Admitted: JUMI 내부 실행 경로에 연결되었고 DAG 실행 준비가 시작될 수 있다.
- Running: 하나 이상의 node가 실행 중이거나 실행을 향해 진행 중이다.
- Succeeded: run이 정상 완료되었다.
- Failed: run이 실패 종료되었다.
- Canceled: run 취소가 terminal state로 확정되었다.

### 3.3 허용 전이

- Accepted -> Admitted
- Accepted -> Canceled
- Admitted -> Running
- Admitted -> Failed
- Admitted -> Canceled
- Running -> Succeeded
- Running -> Failed
- Running -> Canceled

### 3.4 금지 전이

- terminal run state에서 non-terminal state로 되돌아가지 않는다.
- Succeeded -> Failed 같은 terminal 간 전이를 허용하지 않는다.
- Accepted에서 바로 Succeeded로 가지 않는다.

### 3.5 전이 조건

- `Accepted -> Admitted`
  - run이 internal DAG engine에 연결되고 실행 루프에 등록되었을 때
- `Admitted -> Running`
  - 첫 node가 Ready/Releasing/Starting/Running으로 진행되었을 때
- `Admitted -> Failed`
  - 실행 준비 단계에서 복구 불가능한 오류가 발생했을 때
- `Running -> Succeeded`
  - terminal이 아닌 모든 node가 성공적으로 종료되었을 때
- `Running -> Failed`
  - failure policy에 따라 run failure가 확정되었을 때
- `Accepted/Admitted/Running -> Canceled`
  - cancel 요청이 수락되고 run terminal state가 취소로 확정되었을 때

---

## 4. Node State

### 4.1 상태 목록

- Pending
- Ready
- Releasing
- Starting
- Running
- Succeeded
- Failed
- Canceled
- Skipped

### 4.2 상태 의미

- Pending: 아직 dependency가 충족되지 않았거나 실행 가능 판단 전이다.
- Ready: dag-go 기준으로 dependency가 충족되어 실행 가능하다.
- Releasing: bounded release controller가 실제 시작 슬롯을 할당하는 중이다.
- Starting: backend prepare/start 요청이 진행 중이다.
- Running: backend에서 attempt가 실행 중이다.
- Succeeded: node 실행이 성공 종료되었다.
- Failed: node 실행이 실패 종료되었다.
- Canceled: node 실행이 취소 종료되었다.
- Skipped: upstream failure 또는 policy에 의해 실행되지 않기로 확정되었다.

### 4.3 허용 전이

- Pending -> Ready
- Pending -> Skipped
- Pending -> Canceled
- Ready -> Releasing
- Ready -> Canceled
- Releasing -> Starting
- Releasing -> Ready
- Releasing -> Canceled
- Starting -> Running
- Starting -> Failed
- Starting -> Canceled
- Running -> Succeeded
- Running -> Failed
- Running -> Canceled

### 4.4 제한 전이

- retry가 허용된 경우에 한해 `Failed -> Ready` 또는 `Failed -> Releasing` 같은 직접 전이는 두지 않는다.
- retry는 새로운 attempt 생성 후 node를 Ready로 재진입시키는 별도 semantics 문서에서 다룬다.
- 따라서 v0.1 기준으로 node terminal state는 terminal로 취급한다.

### 4.5 금지 전이

- Succeeded에서 다른 state로 이동하지 않는다.
- Skipped에서 Running으로 가지 않는다.
- Pending에서 Running으로 직접 가지 않는다.
- Ready에서 Running으로 직접 가지 않는다.
- Releasing을 거치지 않고 대량 fan-out을 바로 시작하는 의미론을 허용하지 않는다.

### 4.6 전이 조건

- `Pending -> Ready`
  - dag-go readiness가 true가 되었을 때
- `Ready -> Releasing`
  - bounded release controller가 시작 대상 node로 선택했을 때
- `Releasing -> Starting`
  - backend prepare/start가 실제 호출되었을 때
- `Releasing -> Ready`
  - release slot 상실, transient controller condition 등으로 다시 ready queue로 되돌릴 때
- `Starting -> Running`
  - backend에서 start 성공과 실행 관찰 시작이 확인되었을 때
- `Starting -> Failed`
  - prepare/start 단계에서 복구 불가능한 backend 오류가 발생했을 때
- `Running -> Succeeded`
  - backend wait 결과가 성공일 때
- `Running -> Failed`
  - backend wait 결과가 실패일 때
- `Pending/Ready/Releasing/Starting/Running -> Canceled`
  - run cancel 또는 node cancel이 terminal 취소로 확정되었을 때
- `Pending -> Skipped`
  - upstream failure propagation 또는 DAG semantics에 따라 실행 생략이 확정되었을 때

---

## 5. Attempt State

### 5.1 상태 목록

- Prepared
- Started
- Completed
- Errored

### 5.2 상태 의미

- Prepared: attempt identity가 생성되고 backend 실행 준비가 완료되었다.
- Started: backend 실행이 실제로 시작되었다.
- Completed: attempt가 terminal success 또는 terminal cancel을 포함해 정상적으로 종료 처리되었다.
- Errored: attempt lifecycle 중 backend error 또는 observation error가 발생했다.

### 5.3 허용 전이

- Prepared -> Started
- Prepared -> Errored
- Started -> Completed
- Started -> Errored

### 5.4 금지 전이

- Completed에서 다른 state로 이동하지 않는다.
- Errored에서 다른 state로 이동하지 않는다.
- Prepared에서 Completed로 직접 가지 않는다.

### 5.5 전이 조건

- `Prepared -> Started`
  - backend start 성공이 확인되었을 때
- `Prepared -> Errored`
  - prepare 이후 start 이전 단계에서 오류가 확정되었을 때
- `Started -> Completed`
  - backend wait 종료가 success 또는 cancel terminal로 해석되었을 때
- `Started -> Errored`
  - backend wait/observe 과정에서 오류가 확정되었을 때

---

## 6. 상태 간 정합성 규칙

- run이 `Accepted`이면 모든 node는 `Pending` 또는 초기 미생성 상태여야 한다.
- run이 `Running`이면 최소 하나의 node가 `Ready`, `Releasing`, `Starting`, `Running`, 또는 terminal 집계 대기 상태에 있어야 한다.
- node가 `Running`이면 정확히 하나의 current attempt가 `Started` 상태여야 한다.
- node가 `Succeeded`이면 current attempt는 `Completed`여야 한다.
- node가 `Failed`이면 current attempt는 `Completed` 또는 `Errored`일 수 있다.
- node가 `Canceled`이면 current attempt는 `Completed` 또는 `Errored`일 수 있다.
- run이 terminal state이면 모든 node도 terminal 또는 최종 생략 상태여야 한다.

---

## 7. API 노출 원칙

JUMI API는 상태를 단순 문자열 집합으로 노출하지 않는다.
아래의 분리 원칙을 유지해야 한다.

- current execution status
- current bottleneck location
- terminal stop cause
- terminal failure reason

즉, 현재 어디에서 멈춰 있는지와 최종적으로 왜 종료되었는지를 같은 필드로 섞지 않는다.

---

## 8. v0.1 한계

이 문서는 초기 상태 전이 기준선이다.
아래 항목은 후속 문서에서 더 구체화해야 한다.

- retry 재진입 규칙
- cancel race 처리 규칙
- fast-fail에 따른 sibling/downstream 처리 규칙
- optional Kueue observation이 상태 모델에 반영되는 방식
- recovery/replay 시 상태 재구성 규칙

---

## 9. 다음 문서

이 문서 다음으로 바로 필요한 것은 아래와 같다.

- Cancel / Failure / Retry Semantics
- gRPC Contract Draft
- Executable Run Spec Draft
