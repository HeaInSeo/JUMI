# JUMI Sprint Plan

> 작성일: 2026-04-18
> 목적: `poc`에서 검증된 실행 경로를 기반으로 JUMI의 현재 스프린트 순서를 고정한다.

---

## 1. 전제

현재 PoC에서 가장 재사용 가치가 높은 기준선은 아래 경로다.

```text
RunGate -> dag-go -> SpawnerNode -> BoundedDriver -> Kubernetes
```

JUMI는 이 경로를 제품 초기 기준선으로 삼는다.

동시에 아래 사항을 유지한다.

- Redis Streams는 외부 scheduler/front-door 계층에 남긴다.
- authored pipeline과 lowering은 외부 계층에 남긴다.
- Kueue는 optional integration으로 둔다.
- JUMI는 Kubernetes 내부 data-plane app으로 구현한다.
- app 간 제어 인터페이스는 gRPC를 우선한다.

---

## 2. Sprint 목표 구조

초기 JUMI 스프린트는 아래 질문에 답하는 순서로 진행한다.

- JUMI가 no-Kueue 환경에서도 실행 커널로 성립하는가
- JUMI가 run/node/attempt 상태를 일관되게 유지하는가
- JUMI가 gRPC 기반 in-cluster execution service로 동작하는가
- JUMI가 나중에 scheduler/lowering과 연결될 수 있는 경계를 유지하는가

---

## 3. Sprint 1

### 목표

JUMI core skeleton을 고정한다.

### 범위

- gRPC `SubmitRun`
- in-memory registry
- executable run spec Go 타입 정의
- dag-go wiring
- Kubernetes native backend adapter 연결
- bounded release 연결
- `GetRun` 최소 조회
- health / readiness / status endpoint

### 완료 기준

- fixture 기반 executable run spec 하나를 제출할 수 있다.
- A -> B -> C 수준의 기본 DAG가 실행된다.
- Kueue가 없어도 실행된다.
- run 상태가 `Accepted -> Dispatching -> Running -> Succeeded/Failed`로 보인다.

### 참조 PoC

- `pkg/ingress`
- `pkg/store`
- `pkg/adapter`
- `cmd/pipeline-abc`
- `cmd/runner`

---

## 4. Sprint 2

### 목표

상태/이벤트/관찰 모델을 고정한다.

### 범위

- node 상태 모델 추가
- attempt 상태 모델 추가
- 이벤트 기록 추가
- current status / stop cause / failure reason 분리
- `ListRunNodes`
- 기본 cancel API 골격

### 완료 기준

- node별 상태를 읽을 수 있다.
- backend phase 오류가 failure reason으로 보인다.
- release wait와 running을 구분할 수 있다.
- cancel 요청이 아직 시작되지 않은 node를 중단시킨다.

### 참조 PoC

- `pkg/observe`
- `cmd/fastfail`
- `cmd/fanout`

---

## 5. Sprint 3

### 목표

bounded release와 fast-fail semantics를 강화한다.

### 범위

- fan-out saturation 시 release pacing 관찰
- fast-fail semantics 구현 강화
- retry 없음 기본 경로 고정
- no-Kueue e2e 추가
- optional Kueue integration path 실험 시작

### 완료 기준

- fan-out burst에서도 bounded release가 동작한다.
- upstream failure 시 downstream이 `Skipped`로 정리된다.
- no-Kueue e2e가 기준선으로 통과한다.
- Kueue가 있을 때 optional 관찰 신호를 붙일 수 있다.

### 참조 PoC

- `cmd/poc-pipeline`
- `cmd/stress`
- `cmd/fastfail`

---

## 6. Sprint 4

### 목표

cancel과 attempt semantics를 보강한다.

### 범위

- `CancelRun` 강화
- running node best-effort cancel
- attempt ID 도입
- backend prepare/start/wait/cancel phase 매핑
- retry policy field 뼈대 추가

### 완료 기준

- cancel race를 API 수준에서 설명 가능하다.
- node와 attempt가 분리되어 보인다.
- retry 없는 path와 retry 가능한 path의 모델 차이가 명확하다.

---

## 7. Sprint 5

### 목표

외부 연결 준비를 한다.

### 범위

- scheduler adapter boundary 문서화
- executable run spec proto/gRPC 계약 고정
- optional Kueue integration 세분화
- durable registry 후보 정리
- in-cluster deployment skeleton 작성

### 완료 기준

- JUMI가 외부 scheduler와 연결 가능한 northbound contract를 갖는다.
- JUMI 내부 core와 외부 계층 경계가 문서와 코드에서 일치한다.

---

## 8. 병행 작업

### `spawner`

병행 우선순위:

1. Kueue optional화
2. Kubernetes native 기본 경로 보장
3. backend contract 정리
4. observation hardening

### `go-grpc-kit`

병행 우선순위:

1. build/test 복구
2. panic 제거
3. transport helper 역할 고정
4. mTLS/devtls 보조 유틸 안정화

---

## 9. 스프린트 운영 원칙

- JUMI core는 항상 no-Kueue path를 먼저 기준선으로 둔다.
- optional integration은 core를 깨지 않는 범위에서만 붙인다.
- fixture 기반 executable spec 테스트를 authored pipeline보다 우선한다.
- `poc`의 실험 코드는 참고하되, JUMI main path는 더 좁고 명확해야 한다.

---

## 10. 한 줄 결론

JUMI 초기 스프린트의 핵심은 PoC 실행 경로를 그대로 키우는 것이 아니라, 그 경로를 제품 경계에 맞게 더 좁고 단단하게 재구성하는 것이다.
