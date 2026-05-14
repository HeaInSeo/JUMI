# JUMI Scheduler Boundary

> 작성일: 2026-05-14
> 목적: Sprint 5 범위의 scheduler adapter boundary를 현재 코드 기준으로 닫는다.

---

## 1. 한 줄 정의

JUMI는 scheduler가 아니라, 외부 scheduler가 이미 결정한 executable run spec을 Kubernetes 실행으로 변환하고 관찰하는 in-cluster execution data-plane app이다.

---

## 2. Northbound 계약

외부 계층은 JUMI에 아래 정보만 전달해야 한다.

- executable run spec
- run ID
- requester / trace / source metadata
- optional Kueue 힌트

외부 계층이 JUMI에 넘기지 말아야 하는 것은 아래다.

- authored pipeline 원본
- lowering 전 fan-out/fan-in 의미
- queue 운영 정책
- tenant fairness 정책
- retry/backoff 정책의 최종 판단
- Redis Streams consumer state

현재 northbound RPC surface는 다음이다.

- `SubmitRun`
- `GetRun`
- `ListRunNodes`
- `ListNodeAttempts`
- `ListRunEvents`
- `CancelRun`

---

## 3. Scheduler 책임

외부 scheduler 책임:

- 실행 대상 선택
- 우선순위 계산
- 큐잉 / 승인 / fairness
- retry class와 backoff 정책 결정
- authored pipeline lowering
- multi-tenant admission 정책
- idempotent submit orchestration

JUMI 책임:

- run 등록
- DAG 실행
- ready node release pacing
- backend prepare/start/wait/cancel
- fast-fail / cancel propagation
- run/node/attempt 상태 기록
- 이벤트 기록
- optional Kueue 관찰 신호 노출

---

## 4. Southbound 계약

JUMI 내부에서 고정해야 하는 southbound 경계는 다음이다.

- DAG execution engine
- backend adapter
- registry
- metrics / observe / handoff seam

현재 backend adapter는 Kubernetes Job baseline을 기준으로 하고, Kueue는 optional observation/integration이다.

즉 다음 원칙을 유지한다.

- no-Kueue에서도 submit/start/wait/cancel은 성립해야 한다.
- Kueue는 pending reason, admission visibility, pod scheduling 관찰을 강화하는 계층이다.
- Kueue 관련 failure는 core execution failure와 분리해서 해석한다.

---

## 5. Executable Run Spec 경계

Executable run spec은 scheduler와 JUMI 사이의 handoff object다.

이 객체에 포함돼야 하는 것:

- graph
- node image/command/args/env
- resource/timeout/retry skeleton
- cleanup/placement hints
- artifact bindings
- optional Kueue hints

이 객체에 포함되지 않아야 하는 것:

- scheduler 내부 queue shard ID
- authored DSL AST
- policy evaluation trace 전체
- long-lived workflow orchestration state

---

## 6. 현재 코드와의 대응

현재 저장소에서 boundary를 구성하는 주요 지점:

- API layer: `pkg/api`
- spec contract: `pkg/spec`
- execution core: `pkg/executor`
- backend adapter: `pkg/backend`
- registry: `pkg/registry`
- operational entrypoint: `cmd/jumi`

이 구조는 "외부 scheduler -> executable run spec -> JUMI core -> backend" 흐름과 일치한다.

---

## 7. 아직 열어 둔 결정

- northbound proto를 generated public contract로 언제 승격할지
- durable registry 도입 시 replay/idempotency를 어디까지 JUMI 내부 책임으로 둘지
- external scheduler가 cancel dedupe를 보장할지, JUMI가 별도 idempotency key를 받을지

---

## 8. 결론

Sprint 5 기준 scheduler boundary는 다음 문장으로 닫는다.

JUMI는 executable run spec 이후의 execution semantics를 담당하고, 그 이전의 policy scheduling semantics는 외부 계층 책임으로 남긴다.
