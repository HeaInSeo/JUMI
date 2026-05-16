# JUMI spawner Integration Contract Review

> 작성일: 2026-05-16
> 목적: JUMI가 spawner에 기대하는 역할과 하위호환성 관점을 리뷰용으로 고정한다.
> 상태: 리뷰 초안

---

## 1. 핵심 정의

spawner는 JUMI 내부 helper라기보다, 사실상 Kubernetes execution driver 성격을 가진 southbound library다.

즉 spawner는 pure algorithm library라기보다 아래에 가깝다.

- Kubernetes Job execution driver
- runtime integration layer
- operational backend contract

---

## 2. JUMI가 spawner에 맡기는 것

- node spec을 Kubernetes 실행 요청으로 변환
- prepare / start / wait / cancel lifecycle
- Job / Pod 상태 관찰
- output metadata 수집
- runtime helper / output manifest 연계
- optional Kueue-aware 시작 경로

---

## 3. spawner가 pure library가 아닌 이유

spawner는 아래 이유로 pure하게 유지되기 어렵다.

- Kubernetes API 변화 영향
- Job / Pod 상태 모델 변화 영향
- image pull, schedule, runtime failure 같은 환경 이슈
- runtime helper 연동
- output metadata 수집 방식 변화
- Kueue label / workload / pod 관찰

즉 spawner는 semantic invariant보다 operational contract drift가 더 큰 리스크다.

---

## 4. JUMI 쪽 주요 리스크

JUMI는 지금 spawner를 `backend adapter`로 감싸고 있지만, 실제로는 상당히 강하게 의존한다.

예:

- Job start semantics
- cancel propagation
- output metadata availability
- serviceAccount / workingDir / direct start fallback
- Kueue 관련 label 및 관찰

따라서 문서상 generic backend처럼 보이게 쓰더라도, 현실적으로는 spawner-backed Kubernetes path가 baseline contract다.

---

## 5. 하위호환성 리스크

다음 변화는 JUMI에 실질적 파급을 준다.

- prepare/start/wait/cancel 의미 변경
- output metadata contract 변경
- runtime helper contract 변경
- Kueue 관련 label 또는 observation semantics 변경
- direct K8s start fallback behavior 변경

특히 spawner는 "동작은 되지만 상태 의미가 subtly 달라지는" 변화가 위험하다.

---

## 6. JUMI가 기대하는 contract 초안

JUMI는 spawner에 대해 최소한 아래를 기대한다.

1. prepare/start/wait/cancel lifecycle의 안정성
2. backend result를 node/attempt 상태로 환원할 수 있을 만큼의 정보 제공
3. output metadata 수집 실패와 실행 실패를 구분 가능한 계약
4. Kueue 관련 관찰 정보가 core failure와 구분 가능한 형태로 제공될 것
5. Kubernetes native baseline이 Kueue 부재에서도 유지될 것

---

## 7. 에러 지침 관점의 중요성

spawner는 향후 가장 많은 이슈가 발생할 수 있는 계층이다.

그래서 아래 구분이 특히 중요하다.

- spec translation error
- kube api request error
- scheduling / admission delay
- pod startup/runtime error
- cancellation propagation error
- output collection error
- observation-only error

현재 JUMI는 일부를 `backend_prepare_error`, `backend_start_error`, `backend_wait_error` 정도로 묶고 있는데, 장기적으로는 더 세분화된 taxonomy가 필요할 가능성이 높다.

---

## 8. 리뷰 포인트

1. spawner를 사실상 Kubernetes driver로 보는 관점에 동의하는가
2. JUMI가 당분간 generic backend보다 spawner-backed baseline에 더 강하게 의존하는 현실을 문서에 반영해야 하는가
3. output metadata / runtime helper / Kueue observation을 spawner contract의 일부로 인정할 것인가
4. JUMI와 spawner 사이에 별도 compatibility test가 필요한가

---

## 9. 권장 후속 작업

- spawner integration contract를 더 세부 API 단위로 정리
- JUMI failureReason taxonomy와 spawner raw error mapping 표 작성
- Kueue observation을 execution failure와 분리한 상태 모델 점검
