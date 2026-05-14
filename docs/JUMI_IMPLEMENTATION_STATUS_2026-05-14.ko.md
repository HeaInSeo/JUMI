# JUMI Implementation Status

> 기준일: 2026-05-14
> 목적: 현재 구현 상태와 남은 일정의 기준선을 고정한다.

---

## 1. 요약

현재 JUMI는 execution core 기준으로 Sprint 1~4의 핵심을 대부분 구현했고, Sprint 5는 외부 연동 경계와 durable state 방향을 정리하는 단계에 들어와 있다.

AH 연동은 phase-1 seam 기준으로 코드와 테스트가 존재하며, 현재 워킹트리에서도 루트 테스트 스위트는 통과한다.

---

## 2. 완료로 볼 수 있는 범위

### Sprint 1

- `SubmitRun`
- `GetRun`
- in-memory registry
- DAG wiring
- Kubernetes native backend adapter
- bounded release baseline
- HTTP `healthz` / `readyz` / `statusz`
- gRPC in-cluster service entrypoint

### Sprint 2

- run/node/attempt 상태 모델
- 이벤트 기록
- stop cause / failure reason 분리
- `ListRunNodes`
- cancel 기본 골격

### Sprint 3

- fast-fail trigger
- downstream `Skipped` 정리
- bounded release wait 관찰
- optional Kueue admission/pending 신호 관찰

### Sprint 4

- `CancelRun` 강화
- running node best-effort cancel
- attempt ID 도입
- backend prepare/start/wait/cancel phase 매핑
- retry policy field skeleton

### AH phase-1

- artifact binding resolve seam
- output registration seam
- node terminal notify
- sample run finalize / GC evaluate
- 관련 metrics 및 테스트

---

## 3. 남은 일정

### Sprint 5 잔여

- northbound contract를 draft에서 고정 단계로 올릴지 결정
- durable registry 후보를 구현 계획으로 연결
- scheduler/external boundary 문서를 기준선으로 유지
- optional Kueue integration의 제품 범위를 더 잘게 나눌지 결정

### AH 후속

- phase-1 워킹트리 정리/커밋 단위 닫기
- VM lab smoke 재검증 여부 결정
- 실행 spec 및 boundary 문서의 영문 정합성 판단

### 운영 전 남은 기술 부채

- durable state store 부재
- replay/idempotency 부재
- full retention class 모델 미정
- fan-in source priority 고도화 미정

---

## 4. 지금 우선순위

현재 시점에서 우선순위는 아래가 맞다.

1. 문서와 현재 코드의 계약 불일치 제거
2. durable registry 방향 결정
3. external scheduler boundary 고정
4. 그 다음에 durable backend 구현 착수

---

## 5. 검증 상태

2026-05-14 기준 확인:

- `go test ./...` 통과
- AH 관련 워킹트리 변경 존재
- core executor / API / handoff 패키지는 테스트로 살아 있음

---

## 6. 결론

JUMI는 더 이상 skeleton 단계는 아니고, externalization과 durability를 남겨 둔 execution-core 단계다.
