# JUMI Cancel / Failure / Retry Semantics

> 작성일: 2026-04-18
> 목적: JUMI의 cancel, failure propagation, retry 의미론을 현재 기준선으로 고정한다.

---

## 1. 문서 목적

이 문서는 상태 전이 표만으로는 충분히 설명되지 않는 실행 규칙을 시나리오 중심으로 정의한다.

이 문서의 목적은 아래와 같다.

- cancel 요청이 들어왔을 때 JUMI가 무엇을 멈추고 무엇을 그대로 두는지 고정한다.
- node failure가 발생했을 때 downstream과 sibling을 어떻게 처리하는지 고정한다.
- retry를 JUMI 내부의 제한된 execution semantics로 정의한다.
- 외부 policy scheduler가 가져가야 할 책임과 JUMI 내부 책임을 분리한다.

---

## 2. 기본 원칙

- JUMI는 정책 엔진이 아니다.
- JUMI는 복잡한 retry policy/backoff/classification을 내부에서 결정하지 않는다.
- JUMI는 이미 제출된 executable run spec을 안전하게 실행하고, 실패와 취소를 예측 가능하게 전파하는 데 집중한다.
- JUMI core는 no-Kueue 환경에서 완결적으로 동작해야 한다.
- Kueue는 failure propagation의 기준이 아니라 optional observation source다.

---

## 3. Cancel Semantics

### 3.1 cancel의 의미

cancel은 두 가지를 의미한다.

- 더 이상 새로운 node를 시작하지 않는다.
- 이미 시작된 node에 대해서는 backend cancel을 best-effort로 시도한다.

cancel은 이미 완료된 성공 결과를 되돌리는 의미가 아니다.

### 3.2 run cancel 수락 시 기본 규칙

run cancel 요청이 수락되면 아래 규칙을 따른다.

- run은 내부적으로 canceling 흐름으로 진입한다.
- 아직 시작되지 않은 node는 새로 시작하지 않는다.
- `Pending`, `Ready`, `Releasing` node는 `Canceled` 또는 `Skipped` terminal 처리 대상이 된다.
- `Starting`, `Running` node에는 backend cancel을 best-effort로 요청한다.
- 이미 `Succeeded`, `Failed`, `Canceled`, `Skipped`인 node는 그대로 둔다.
- 모든 in-flight attempt가 terminal 처리되면 run을 `Canceled`로 확정한다.

### 3.3 node 상태별 cancel 규칙

- `Pending`
  - downstream 또는 future work로 남아 있는 상태이므로 즉시 실행 대상에서 제외한다.
  - 일반적으로 `Canceled` 또는 실패 전파 규칙에 따른 `Skipped`로 닫는다.
- `Ready`
  - release queue에서 제거한다.
  - 새로 시작하지 않고 `Canceled`로 닫는다.
- `Releasing`
  - release slot을 반납한다.
  - backend start 이전이면 `Canceled`로 닫는다.
- `Starting`
  - backend start와 cancel이 경합할 수 있다.
  - JUMI는 backend cancel을 best-effort로 보낸다.
  - 결과 해석은 completion race를 허용한다.
- `Running`
  - backend cancel을 best-effort로 보낸다.
  - backend가 성공 종료를 먼저 반환하면 `Succeeded`로 남을 수 있다.
  - backend가 cancel terminal을 반환하면 `Canceled`로 닫는다.

### 3.4 cancel race 허용 규칙

JUMI는 아래 race를 허용한다.

- cancel 요청 직후 backend가 성공 완료를 먼저 반환하는 경우
- cancel 요청 직후 backend가 실패 완료를 먼저 반환하는 경우
- cancel 요청이 전송되었지만 backend가 이미 terminal인 경우

이 경우 JUMI는 backend terminal 결과를 진실값으로 취급한다.
즉 cancel을 요청했다고 해서 모든 in-flight node가 반드시 `Canceled`가 되지는 않는다.

### 3.5 cancel 완료 기준

run cancel은 아래 중 하나가 성립했을 때 terminal로 본다.

- 모든 active attempt가 terminal state가 되었을 때
- 시스템이 더 이상 새로운 backend activity가 발생하지 않음을 확정했을 때

run terminal state는 `Canceled`로 기록한다.
다만 개별 node는 `Succeeded`, `Failed`, `Canceled`, `Skipped`가 혼재할 수 있다.

---

## 4. Failure Propagation Semantics

### 4.1 기본 failure policy

현재 기본 failure policy는 `fail-fast`다.

의미:

- 어떤 node가 terminal failure가 되면 새로운 downstream 실행을 더 이상 시작하지 않는다.
- 이미 완료된 성공 node는 되돌리지 않는다.
- 이미 실행 중인 sibling은 즉시 kill 대상이 아니라, 별도 cancel 규칙에 따라 처리한다.

### 4.2 downstream 처리 규칙

upstream node failure가 확정되면 아래 규칙을 따른다.

- 아직 시작되지 않은 downstream node는 실행하지 않는다.
- 이런 downstream node는 `Skipped`로 마감한다.
- `Skipped`의 이유는 terminal failure reason과 별도로 dependency failure 계열로 기록한다.

### 4.3 sibling 처리 규칙

같은 fan-out 단계의 sibling에 대해서는 아래 원칙을 따른다.

- 이미 `Running`인 sibling은 기본적으로 그대로 둔다.
- 정책이나 spec이 best-effort sibling cancel을 허용하면 cancel을 시도할 수 있다.
- 기본 기준선에서는 running sibling을 자동 강제 취소하지 않는다.

이 기준은 PoC에서 확인된 단순 fail-fast 경로와도 맞는다.

### 4.4 run failure 확정 규칙

run은 아래 상황에서 `Failed`가 된다.

- non-retriable node failure가 발생했고 failure policy가 run 전체 실패를 요구할 때
- retriable node가 retry exhaustion에 도달했을 때
- 실행 준비 단계에서 복구 불가능한 내부 오류가 발생했을 때

run failure가 확정되면:

- 새 node release를 중단한다.
- 남은 non-started node는 `Skipped` 또는 `Canceled`로 정리한다.
- active node는 별도 cancel policy에 따라 best-effort cancel 대상이 될 수 있다.

### 4.5 failure reason 분리 원칙

terminal failure reason은 아래처럼 계층적으로 분리한다.

- validation_error
- backend_prepare_error
- backend_start_error
- backend_wait_error
- retry_exhausted
- dependency_failed
- cancellation_propagated
- internal_error

즉, 현재 어디에서 기다리고 있는지와 최종적으로 왜 실패했는지를 같은 필드로 섞지 않는다.

---

## 5. Retry Semantics

### 5.1 retry의 위치

retry는 JUMI 내부에서 제한된 execution semantics로만 존재한다.

JUMI가 다루는 것:

- attempt를 새로 만들 수 있는가
- retry 횟수가 남아 있는가
- node를 다시 `Ready`로 재진입시킬 수 있는가

JUMI가 다루지 않는 것:

- 복잡한 backoff 계산
- failure classification policy
- tenant-aware retry fairness
- queue priority를 고려한 retry ordering

이런 정책은 외부 scheduler 계층 책임이다.

### 5.2 기본값

- 기본 retry 동작은 `no-retry`다.
- retryPolicy가 있을 때만 node 단위 retry를 허용한다.
- retry는 node 단위이며, run 전체 retry는 다루지 않는다.

### 5.3 retry 가능한 실패 범주

현재 기준선에서는 아래 실패를 같은 retry bucket으로 본다.

- prepare error
- start error
- wait error

이후 더 세분화할 수 있지만, 현재는 execution semantic 단순성을 우선한다.

### 5.4 retry 동작 규칙

retry가 허용된 node에서 실패가 발생하면 아래 규칙을 따른다.

- current attempt는 `Completed` 또는 `Errored`로 terminal 처리한다.
- 새 attempt ID를 발급한다.
- node를 terminal `Failed`로 닫지 않고 다시 `Ready`로 재진입시킨다.
- 재진입 후에는 bounded release 규칙을 동일하게 적용한다.
- maxAttempts를 모두 소진하면 그때 node를 `Failed`로 terminal 처리한다.

### 5.5 retry exhaustion과 run failure

retry exhaustion이 발생하면 아래 규칙을 따른다.

- node는 `Failed`가 된다.
- failure reason은 `retry_exhausted`로 기록할 수 있다.
- run failure policy가 fail-fast이면 그 시점부터 downstream skip과 run failure 확정 규칙을 적용한다.

### 5.6 cancel과 retry의 관계

cancel이 먼저 수락된 run에서는 새로운 retry attempt를 만들지 않는다.
즉 cancel 이후에는 retry budget이 남아 있어도 재시도하지 않는다.

---

## 6. 시나리오 규칙

### 6.1 단순 실패

시나리오:

- `A -> B -> C`
- `B`가 wait failure로 종료
- retry 없음

결과:

- `B = Failed`
- `C = Skipped`
- `run = Failed`

### 6.2 retry 후 성공

시나리오:

- `A -> B -> C`
- `B`가 첫 attempt에서 start error
- `maxAttempts = 2`
- 두 번째 attempt 성공

결과:

- `B`는 node 수준에서 terminal `Succeeded`
- 첫 attempt는 `Errored` 또는 `Completed`
- 두 번째 attempt는 `Completed`
- `C`는 정상 시작 가능
- `run`은 계속 진행 가능

### 6.3 running 중 cancel

시나리오:

- `A`, `B`, `C` 중 `B`가 Running
- run cancel 요청 수락

결과:

- 아직 시작되지 않은 node는 시작하지 않음
- `B`에는 backend cancel best-effort 요청
- `B`가 성공 완료를 먼저 반환하면 `Succeeded` 가능
- 최종 run terminal은 `Canceled`

### 6.4 fan-out 중 하나 실패

시나리오:

- `A -> (B1, B2, B3) -> C`
- `B2`가 실패
- `B1`, `B3`는 이미 Running

결과:

- `C`는 시작하지 않음, `Skipped`
- `B1`, `B3`는 기본 기준선에서는 계속 실행 가능
- 필요 시 별도 policy로 best-effort cancel 확장 가능
- run은 active sibling 정리 후 `Failed`

---

## 7. Kueue와의 관계

- cancel, failure, retry semantics는 Kueue 존재 여부와 독립적으로 성립해야 한다.
- Kueue는 admission visibility와 pending reason 관찰을 강화하는 선택 기능이다.
- Kueue 부재를 이유로 retry/cancel/failure semantics가 달라져서는 안 된다.

---

## 8. 다음 문서

이 문서 다음으로 바로 필요한 것은 아래와 같다.

- JUMI gRPC Contract Draft
- Executable Run Spec Draft
