# JUMI dag-go Integration Contract Review

> 작성일: 2026-05-16
> 목적: JUMI가 dag-go에 기대하는 역할과 하위호환성 관점을 리뷰용으로 고정한다.
> 상태: 리뷰 초안

---

## 1. 핵심 정의

dag-go는 JUMI의 내부 구현 디테일이 아니라, JUMI가 의존하는 execution primitive library다.

JUMI가 dag-go를 통해 얻고 싶은 것은 아래에 가깝다.

- DAG validity
- dependency wiring
- ready node progression
- runner execution progression

즉 dag-go는 full orchestration engine보다 readiness/dependency kernel에 가깝다.

---

## 2. JUMI가 dag-go에 맡기는 것

- graph shape validation 일부
- edge 연결
- start node에서 ready node로의 progression
- runner dispatch / wait progression

---

## 3. JUMI가 dag-go에 맡기지 않는 것

- run/node/attempt 상태 모델
- cancel semantics 최종 의미
- fast-fail semantics 최종 의미
- AH resolve/register/finalize/evaluate seam
- artifact-aware execution semantics
- Kubernetes / Kueue observation
- northbound API contract

즉 dag-go는 execution kernel의 일부지만, 제품 의미론 전체는 아니다.

---

## 4. pure 방향에 대한 해석

현재 논의 기준으로 dag-go는 가능한 한 pure하게 가는 것이 바람직하다.

의미:

- Kubernetes 의존을 갖지 않는다
- AH 같은 외부 seam 의미를 알지 않는다
- retry policy, artifact policy, queue policy를 직접 내장하지 않는다
- graph/readiness/execution primitive 성격을 유지한다

이 방향의 장점:

- JUMI 외 다른 상위 계층에서도 재사용 가능
- 하위호환성 판단 기준이 더 명확해짐
- 운영 환경 변화보다 semantic contract에 집중 가능

---

## 5. 하위호환성 리스크

dag-go는 pure library로 가더라도 아래 변경은 JUMI에 큰 영향을 준다.

- ready 판단 순서가 달라지는 변경
- runner lifecycle callback 순서 변경
- wiring / edge validation 의미 변경
- `Wait()` / cancellation 전파 의미 변경
- concurrency model 변화

즉 dag-go는 pure하기 때문에 오히려 "작은 semantic drift"가 상위 JUMI 의미론을 깨뜨릴 수 있다.

---

## 6. JUMI가 기대하는 contract 초안

JUMI는 dag-go에 대해 최소한 아래 contract를 기대한다.

1. dependency readiness 판단의 기준선 제공
2. invalid DAG는 실행 전 에러로 식별 가능
3. runner registration contract가 안정적일 것
4. execution cancellation이 상위 context와 정합적으로 연결될 것
5. runner progression 의미가 minor release 수준에서 쉽게 깨지지 않을 것

---

## 7. 리뷰 포인트

1. dag-go를 readiness/dependency kernel로 규정하는 데 동의하는가
2. JUMI가 fast-fail/cancel/attempt 의미론을 계속 소유하는 데 동의하는가
3. 향후 retry hook 같은 기능이 dag-go 안으로 들어가면 안 된다는 선을 어디까지 그을 것인가
4. JUMI와 dag-go 사이에 별도 contract test가 필요한가

---

## 8. 권장 후속 작업

- dag-go contract test 초안 작성
- JUMI가 의존하는 dag-go semantic invariant 목록화
- minor/major version compatibility policy 정의
