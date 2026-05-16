# JUMI Locality Semantics Review

> 작성일: 2026-05-16
> 목적: JUMI가 locality를 scheduler 보장이 아니라 runtime hint로 다룬다는 기준을 리뷰용으로 고정한다.

---

## 1. 한 줄 정의

JUMI에서 locality는 실행 성공 조건이 아니라, 더 싼 경로를 유도하기 위한 soft hint다.

즉:

- preferred node에 배치되면 local path를 쓴다.
- preferred node에 배치되지 않아도 즉시 실패하지 않는다.
- locality miss는 remote fetch / materialization fallback 후보로 해석한다.

---

## 2. 책임 경계

AH 책임:

- artifact handoff 시점의 preferred locality intent 제공
- remote fetch / materialization 결정을 위한 resolution seam 제공

JUMI 책임:

- preferred locality intent를 node 실행 컨텍스트로 전달
- 실제 pod placement를 관찰
- locality matched / missed를 기록
- miss 시 fallback 경로를 실행 의미론으로 해석

Kubernetes scheduler 책임:

- 실제 node placement 최종 결정
- preferred node를 만족시키지 못할 수도 있음

따라서 JUMI는 locality-aware executor일 수는 있어도, placement authority는 아니다.

---

## 3. 상태 의미론

`locality miss`는 별도 terminal state가 아니다.

추천 기준:

- `NodeStatus` / `RunStatus`는 기존 축을 유지한다.
- locality는 attempt/node 관찰 컨텍스트와 event로 남긴다.
- 실제 실패는 fallback 체인 어디서 깨졌는지로 분류한다.

예:

- preferred node 일치 + 성공 → 정상 성공
- preferred node 불일치 + fallback 성공 → 정상 성공
- preferred node 불일치 + fallback 실패 → node 실패

---

## 4. 이벤트 기준

최소 이벤트:

- `node.locality.preferred`
- `node.locality.matched`
- `node.locality.missed`
- `node.locality.fallback_started`
- `node.locality.fallback_succeeded`
- `node.locality.fallback_failed`

해석 원칙:

- `missed`는 운영 경고일 수는 있어도 즉시 실패는 아니다.
- `fallback_failed`부터 실제 node 실패 맥락으로 본다.

---

## 5. 메트릭 기준

최소 카운터:

- `jumi_locality_preferred_total`
- `jumi_locality_matched_total`
- `jumi_locality_miss_total`
- `jumi_locality_fallback_started_total`
- `jumi_locality_fallback_succeeded_total`
- `jumi_locality_fallback_failed_total`

운영에서 중요한 비율:

- miss rate = `locality_miss / locality_preferred`
- fallback success rate = `locality_fallback_succeeded / locality_fallback_started`

이 두 비율이 있어야:

- locality hint가 실제로 얼마나 맞는지
- locality miss가 나도 시스템이 얼마나 버티는지

를 판단할 수 있다.

---

## 6. 실패 reason 기준

`locality miss` 자체는 failure reason이 아니다.

실패 reason은 아래처럼 fallback 체인의 실제 실패 지점에 매핑한다.

- `resolve_handoff_error`
- `input_resolution_missing`
- `backend_prepare_error`
- `backend_start_error`
- `backend_wait_error`
- 이후 필요 시:
  - `input_fetch_error`
  - `input_materialization_error`

즉 “같은 노드에 못 붙었다”는 배경 정보이고, terminal failure는 실제 실행 실패 지점으로 남겨야 한다.

---

## 7. 현재 구현에 대한 리뷰 포인트

현재 JUMI는 이미:

- AH resolve seam
- `remote_fetch` / materialization 계측
- Kueue / scheduler 관찰

을 가지고 있다.

여기서 추가로 중요한 것은:

- preferred node와 actual pod node를 비교해 locality miss를 기록하는 것
- miss를 실패가 아니라 fallback 맥락으로 해석하는 것

이다.

---

## 8. 결론

JUMI의 locality 의미론은 다음 문장으로 닫는 것이 적절하다.

JUMI는 locality를 soft hint로만 다루고, locality miss는 관찰 가능한 runtime variance로 기록하며, 실제 성공/실패 판정은 fallback 경로의 결과로 결정한다.
