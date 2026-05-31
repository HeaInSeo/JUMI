# Deferred Work Handoff Plan

작성일: 2026-05-31  
상태: Active Handoff Plan  
목적: 남은 deferred item을 가장 작은 스프린트 단위로 분해해서 다음 agent가 중간에 흐트러지지 않고 이어서 진행할 수 있도록 한다.

## 1. 현재 완료 기준선

이미 완료된 큰 기준선:

- Sprint 3A remote_fetch happy path
- Sprint 3B same-node local_reuse happy path
- Sprint 3C-1 source registry 저장 모델
- Sprint 3C-2 candidate planner
- Sprint 3C-3A~3E security / parity / fail-closed hardening
- Sprint 3C-4A node-contract input baseline
- Sprint 3D-1 AddSource lifecycle minimal API
- Sprint 3D-2 source verifier minimal API
- Sprint 3D-3 local verification baseline
- Sprint 3D-4 remote smoke verification baseline
- Sprint 3D-5 sample run lifecycle query seam

현재 정본 기준:

- JUMI `main`
- artifact-handoff `main`
- node-artifact-runtime `main`

## 2. 아직 남은 deferred item

남은 큰 항목:

1. cleanup / TTL / eviction
2. post-scheduling `ResolveBinding(targetNodeName=actualNode)`
3. signed URL 지원 정책과 runtime-only 처리
4. `local_reuse` zero-copy 전략
5. 새 backend
6. full CI wiring

## 3. 권장 진행 순서

다음 agent는 아래 순서대로 진행하는 것을 권장한다.

### Sprint 3D-6

이름:

`cleanup / TTL baseline`

이유:

- `GetSampleRunLifecycle` seam이 이제 JUMI에서 조회 가능하다.
- lifecycle 조회를 기반으로 최소 cleanup 판단 기준을 만들 수 있다.
- 구조 변경 없이 작은 범위로 닫기 쉽다.

이번 스프린트에서 해야 할 것:

- sample run lifecycle 조회를 사용하는 최소 관리/관찰 경로 추가
- TTL/retention 상태를 확인할 수 있는 verification command 또는 small helper 추가
- cleanup 실행 controller는 넣지 않음
- artifact 실제 삭제는 넣지 않음

이번 스프린트에서 하지 말 것:

- 실제 artifact deletion
- background cleanup loop
- kube CronJob/controller
- post-scheduling re-resolve

완료 기준:

- sample run lifecycle 조회 결과에서
  - finalized
  - retention_until
  - gc_eligible
  - gc_blocked_reason
  를 재현 가능하게 확인할 수 있다.
- 문서에 operator verification command가 남는다.

### Sprint 3E

이름:

`post-scheduling ResolveBinding minimum`

이유:

- 가장 큰 구조 변경이다.
- cleanup baseline보다 먼저 넣으면 디버깅 범위가 너무 커진다.

이번 스프린트 최소 목표:

- actual scheduled node 관찰
- `ResolveBinding(targetNodeName=actualNode)` 재호출 seam 추가
- fallback 선택 자체는 최소 범위로 제한

이번 스프린트에서 하지 말 것:

- multi-stage retry loop 확대
- new backend fallback
- large refactor

### Sprint 3F

이름:

`signed URL policy`

최소 목표:

- 현재 query reject 정책 이후의 signed URL 처리 방침 확정
- metadata 저장 금지 / runtime-only 허용 여부 결정

### Sprint 3G

이름:

`local_reuse strategy decision`

최소 목표:

- `copy default` 유지 여부 재확인
- `reflink optional`
- `hardlink explicit opt-in`
- `copy fallback`

### Sprint 3H

이름:

`CI wiring baseline`

최소 목표:

- 이미 있는 verification command를 CI job으로 연결
- Harbor/GHCR sync smoke를 manual gate 또는 scheduled gate로 연결

## 4. 다음 agent가 바로 시작할 가장 작은 작업

다음 agent는 아래 한 가지부터 시작하는 것을 권장한다.

### Start Here

`Sprint 3D-6A | lifecycle retention visibility baseline`

작업 범위:

- JUMI에서 handoff client의 `GetSampleRunLifecycle`를 호출하는 최소 utility 또는 verification path 추가
- sample run 하나에 대해 lifecycle snapshot을 출력/기록하는 작은 경로 추가
- 문서에 operator command 추가

수정 후보:

- `pkg/handoff/*`
- `pkg/executor/*` 또는 small helper
- `Makefile`
- `docs/JUMI_REMOTE_FETCH_V0_SPRINT_PLAN.md`

테스트 후보:

- `pkg/handoff` focused tests
- 필요 시 small executor unit test

완료 기준:

- lifecycle snapshot 조회를 사람이 재현 가능
- cleanup/TTL controller 없이도 retention/gc 상태를 눈으로 확인 가능

## 5. scope creep 방지

다음 agent는 아래를 한 스프린트에 섞지 않는다.

- cleanup baseline과 post-scheduling re-resolve를 같이 하지 않음
- signed URL 정책과 new backend를 같이 하지 않음
- zero-copy 전략과 CI wiring을 같이 하지 않음
- actual artifact deletion은 cleanup visibility baseline에 포함하지 않음

## 6. 운영 메모

- Harbor primary / GHCR backup sync 정책은 유지한다.
- smoke와 `k8sgpt` lint는 현재 기준선을 깨지 않는 선에서만 확장한다.
- node-contract canonical input baseline은 이미 올라가 있으므로, 새 작업은 가능하면 contract 기준으로 붙인다.

## 7. 한 줄 결론

다음 작업은 큰 기능을 더 넣는 것이 아니라,  
`Sprint 3D-6A | lifecycle retention visibility baseline`처럼 가장 작은 cleanup/TTL 전 단계부터 시작하는 것이 가장 안전하다.
