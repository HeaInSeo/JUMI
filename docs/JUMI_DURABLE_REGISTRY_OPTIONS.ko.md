# JUMI Durable Registry Options

> 작성일: 2026-05-14
> 목적: Sprint 5 범위의 durable registry 후보를 현재 기준으로 정리한다.

---

## 1. 현재 상태

현재 JUMI registry는 in-memory 구현만 가진다.

의미:

- 프로세스 재시작 시 run/node/attempt/event 상태는 사라진다.
- replay, recovery, idempotent resubmit은 아직 제품 경계 밖이다.
- 지금 구현은 execution core 의미론 검증에는 충분하지만 운영 기준선으로는 부족하다.

---

## 2. 요구사항

durable registry는 최소한 아래를 만족해야 한다.

- run/node/attempt 현재 상태 저장
- 이벤트 append-only 기록
- run 단위 조회 효율
- cancel/update 동시성 안전성
- 재시작 후 상태 복구 가능성
- 향후 replay/idempotency 확장 가능성

추가로 바람직한 특성:

- Kubernetes 내부 배포 친화성
- Go 구현 복잡도 관리 가능
- 운영자가 이해 가능한 failure mode

---

## 3. 후보

### 후보 A: PostgreSQL

장점:

- run/node/attempt/event를 관계형으로 다루기 쉽다.
- optimistic locking, unique key, index 전략이 명확하다.
- 조회 API와 운영 쿼리 작성이 쉽다.
- replay/idempotency 키 확장에 유리하다.

단점:

- 별도 stateful dependency 운영이 필요하다.
- write path를 잘못 설계하면 event + snapshot 이중 쓰기 복잡성이 생긴다.

판단:

- 현재 JUMI 요구사항과 가장 잘 맞는 기본 후보다.

### 후보 B: Kubernetes CRD

장점:

- 클러스터 내부 기본 제어면에 자연스럽게 붙는다.
- 별도 외부 DB 없이 시작할 수 있다.

단점:

- attempt/event 기록량이 많아질수록 오브젝트 비대화가 쉽다.
- append-only event 모델과 조회 패턴이 불편하다.
- 상태 전이와 이벤트 히스토리를 함께 다루기 어렵다.

판단:

- control-plane 오브젝트 노출에는 유리하지만 JUMI registry의 주 저장소로는 부적합하다.

### 후보 C: Redis

장점:

- 빠르고 단순한 key/value 및 stream 저장이 가능하다.
- front-door queue 계층과 붙이기 쉽다.

단점:

- JUMI가 Redis Streams에 의미적으로 다시 종속될 위험이 있다.
- 상태 스냅샷 + 이벤트 + 조회 모델을 장기적으로 다루기 까다롭다.
- 내구성/운영성 판단이 별도 설계에 크게 의존한다.

판단:

- cache 또는 단기 보조 저장소 후보는 될 수 있어도 primary durable registry 기본안으로는 보수적이지 않다.

---

## 4. 권장안

현재 기준 권장안은 아래 순서다.

1. PostgreSQL을 durable registry 1순위 후보로 채택
2. registry interface는 유지하고 memory/postgres 이중 구현 구조로 확장
3. snapshot table과 event table을 분리
4. replay/idempotency는 1차 도입 범위에서 최소형으로만 포함

권장 schema 방향:

- `runs`
- `run_nodes`
- `node_attempts`
- `run_events`

---

## 5. 비범위

이번 단계에서 아직 하지 않는 것:

- full event sourcing 전환
- workflow engine 수준 replay
- external scheduler 전체 복구 대체
- tenant policy history 저장

---

## 6. 다음 구현 순서

1. `registry.Registry` 계약을 durable backend 대응 가능하게 유지
2. 상태 업데이트 경로별 required atomicity 목록 정리
3. PostgreSQL schema 초안 작성
4. `CreateRun`, `UpdateRun`, `UpdateNode`, `UpsertAttempt`, `AppendEvent`의 transaction boundary 정의
5. memory/postgres 공통 테스트 세트 작성

---

## 7. 결론

Sprint 5 기준 durable registry 결정은 "지금은 memory only, 다음 durable 후보는 PostgreSQL 우선"으로 닫는다.
