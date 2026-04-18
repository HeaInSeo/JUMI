# JUMI Final Development Goal

> 작성일: 2026-04-18
> 목적: JUMI 개발의 최종 목표를 제품 관점에서 한 문서로 고정한다.

---

## 1. 최종 목표 한 줄

JUMI의 최종 개발 목표는 Kubernetes 클러스터 내부에서 executable run spec을 받아 run, node, attempt 단위를 안정적으로 실행·제어·관찰하는 policy-agnostic execution data-plane app을 만드는 것이다.

---

## 2. 최종 목표의 의미

이 목표는 아래를 뜻한다.

- JUMI는 authored pipeline compiler가 아니다.
- JUMI는 policy scheduler가 아니다.
- JUMI는 front-door queue system이 아니다.
- JUMI는 executable run spec 기반 execution kernel이다.
- JUMI는 실행 요청의 정책적 선택을 하지 않고, 이미 선택된 executable run spec을 실제 Kubernetes 실행으로 변환·관찰·제어하는 data-plane 계층이다.
- JUMI는 Kubernetes native path 위에서 먼저 성립해야 한다.
- Kueue는 선택적으로 연동되지만, 필수 의존성이 아니다.

---

## 3. 제품 수준 완료 모습

최종적으로 JUMI는 아래 모습을 가져야 한다.

### 3.1 실행 경계

- northbound로 executable run spec submit/status/cancel을 위한 gRPC app-to-app 인터페이스를 가진다.
- southbound로 internal DAG engine (dag-go), backend adapter, Kubernetes API와 연결된다.
- southbound에서 optional Kueue observation을 통해 admission visibility와 pending reason 관찰을 강화할 수 있다.
- Redis Streams, scheduler, lowering은 외부 계층으로 남긴다.

### 3.2 실행 의미론

- dag-go를 readiness source of truth로 사용한다.
- ready node를 bounded release로 제어한다.
- backend prepare/start/wait/cancel을 일관되게 수행한다.
- fast-fail, cancel semantics를 명확히 가지며, retry는 제한된 execution semantics만 내부에서 다루고 복잡한 retry policy와 backoff/classification은 외부 계층에 남긴다.

### 3.3 상태 모델

- run/node/attempt 상태를 일관된 전이 규칙으로 유지하고 API로 노출한다.
- current execution status, current bottleneck location, terminal stop cause, terminal failure reason을 분리한다.
- 현재 상태와 이벤트 히스토리를 섞지 않는다.

### 3.4 운영성

- health / readiness / status endpoint를 가진다.
- JUMI core는 no-Kueue 환경에서 완결적으로 동작한다.
- Kueue integration은 admission visibility, pending reason, cluster policy alignment를 강화하는 선택 기능이다.
- 운영자가 병목 위치를 읽을 수 있다.

### 3.5 확장성

- 향후 external scheduler와 연결 가능하다.
- 향후 pipeline lowering layer와 연결 가능하다.
- 향후 durable registry와 replay/idempotency를 강화할 수 있다.

---

## 4. 최종 산출물 기준

JUMI 개발이 충분히 진행되었다고 판단하려면 최소한 아래가 성립해야 한다.

- no-Kueue 기본 실행 경로가 안정적으로 동작한다.
- optional Kueue path가 core를 깨지 않고 붙는다.
- fixture 기반 executable run spec 샘플들이 통합 테스트로 유지된다.
- gRPC submit/status/cancel 계약이 고정된다.
- run/node/attempt 상태가 일관된 전이 규칙 아래 API로 조회 가능하다.
- cancel과 failure propagation이 예측 가능하다.

---

## 5. 비목표

JUMI 최종 목표에 포함되지 않는 것은 아래와 같다.

- authored pipeline 직접 해석
- policy priority/fairness 계산
- Redis Streams consumer group 운영
- tenant scheduling policy
- queue selection policy
- control-plane orchestration 전체 대체
  - JUMI는 상위 control-plane의 정책, 승인, 큐잉, 오케스트레이션 책임을 흡수하지 않는다.

---

## 6. 최종 목표 문장

영문:

JUMI is a policy-agnostic in-cluster execution data-plane app that accepts executable run specs and manages run, node, and attempt execution on Kubernetes with optional Kueue integration.

국문:

JUMI는 executable run spec을 받아 Kubernetes 위에서 run, node, attempt 실행을 제어·관찰하는 in-cluster policy-agnostic execution data-plane app이며, Kueue는 선택적으로 연동된다.
