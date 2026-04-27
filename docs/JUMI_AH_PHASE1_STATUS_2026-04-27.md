# JUMI AH Phase-1 Status

기준일: `2026-04-27`

## 목적

이 문서는 현재 JUMI 저장소에 올라와 있는
`artifact-handoff` seam phase-1 구현 범위를 고정한다.

목표는 새 기능을 더 넓히는 것이 아니라,
이미 들어간 handoff seam, sample-run lifecycle hook,
기본 metrics와 테스트 범위를 설명 가능한 상태로 닫는 것이다.

## 현재 포함된 범위

### 1. Spec 확장

- `run.sampleRunId` 추가
- `node.artifactBindings[]` 추가
- `artifactBindings` validation 추가

관련 파일:

- `pkg/spec/types.go`
- `pkg/spec/validate.go`

### 2. Executor phase 확장

- `BuildingBindings`
- `ResolvingInputs`

artifact binding이 존재하는 node는
backend prepare 전에 AH resolve seam을 먼저 통과한다.

관련 파일:

- `pkg/executor/executor.go`

### 3. AH client seam

현재 phase-1 client는 다음 경로를 가진다.

- `ResolveBinding`
- `RegisterArtifact`
- `NotifyNodeTerminal`
- `FinalizeSampleRun`
- `EvaluateGC`

현재는 HTTP shim 기준 구현이며,
`JUMI_AH_URL`이 비어 있으면 noop client를 사용한다.

관련 파일:

- `pkg/handoff/client.go`
- `cmd/jumi/main.go`

### 4. Node output registration

node가 성공하면 `outputs[]`를 기준으로
AH에 artifact register를 보낸다.

기본 URI 형식:

- `jumi://runs/<run>/nodes/<node>/outputs/<output>`

관련 파일:

- `pkg/executor/executor.go`

### 5. Terminal / finalize / GC hook

node terminal 시점에는 `NotifyNodeTerminal`,
run terminal 시점에는 `FinalizeSampleRun`, `EvaluateGC`를 호출한다.

현재 metrics는 실제 호출이 성공했을 때만 증가한다.

관련 파일:

- `pkg/executor/executor.go`

### 6. Metrics

현재 phase-1에서 노출하는 최소 metrics:

- `jumi_jobs_created_total`
- `jumi_fast_fail_trigger_total`
- `jumi_cleanup_backlog_objects`
- `jumi_input_resolve_requests_total`
- `jumi_input_remote_fetch_total`
- `jumi_input_materializations_total`
- `jumi_artifacts_registered_total`
- `jumi_sample_runs_finalized_total`
- `jumi_gc_evaluate_requests_total`

관련 파일:

- `pkg/metrics/metrics.go`
- `pkg/metrics/metrics_test.go`

### 7. 테스트 범위

현재 phase-1 테스트는 아래를 덮는다.

- artifact binding resolve path
- HTTP handoff client path
- output registration path
- terminal/finalize/GC hook path
- metric 노출 확인

관련 파일:

- `pkg/executor/dag_engine_test.go`
- `pkg/executor/dag_engine_handoff_http_test.go`
- `pkg/handoff/client_test.go`

## 현재 진입점

- binary: `cmd/jumi`
- env:
  - `JUMI_AH_URL`
  - `JUMI_HTTP_ADDR`
  - `JUMI_GRPC_ADDR`
  - `JUMI_NAMESPACE`
  - `JUMI_KUBECONFIG`

HTTP endpoints:

- `GET /healthz`
- `GET /readyz`
- `GET /statusz`
- `GET /metrics`

## 현재 판단

phase-1 기준으로 보면,
JUMI는 이제 단순 DAG executor만은 아니다.

현재 기준선은 다음까지는 설명 가능하다.

- artifact-aware binding을 읽는다
- AH resolve/register/finalize/evaluate seam을 호출한다
- sample-run scope를 최소형으로 유지한다
- kube-slint가 읽을 최소 counter/gauge를 노출한다

다만 아직 아래 단계는 phase-1 범위 밖이다.

- durable state store
- provenance manifest 생성
- full retention class 모델
- fan-in source priority 고도화
- dev-space 연동

## 남은 정리 포인트

- 현재 워킹트리를 phase-1 단위 커밋으로 닫기
- VM lab smoke를 현재 seam 기준으로 다시 확인할지 결정
- 실행 spec 문서를 더 넓게 영어 문서와도 맞출지 판단
