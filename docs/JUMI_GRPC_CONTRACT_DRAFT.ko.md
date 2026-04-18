# JUMI gRPC Contract Draft

> 작성일: 2026-04-18
> 목적: JUMI northbound gRPC app-to-app 계약의 초기 초안을 고정한다.

---

## 1. 문서 목적

이 문서는 JUMI가 외부 계층과 통신할 때 사용하는 northbound gRPC 계약의 최소 범위를 정의한다.

현재 목표는 아래와 같다.

- executable run spec submit 계약을 고정한다.
- run / node 상태 조회 계약을 고정한다.
- cancel 계약을 고정한다.
- health/readiness/status는 운영용 HTTP endpoint로 남기고, gRPC 계약에서는 실행 제어 중심 API만 다룬다.

---

## 2. 설계 원칙

- northbound 계약은 app-to-app gRPC를 우선한다.
- northbound 계약은 authored pipeline이 아니라 executable run spec만 받는다.
- API 응답은 등록 성공과 실행 완료를 구분한다.
- 현재 상태와 최종 종료 원인을 분리해서 노출한다.
- optional Kueue 정보는 부가 필드로만 다룬다.
- Kueue 부재를 이유로 필수 응답 필드가 비정상화되어서는 안 된다.

---

## 3. 서비스 개요

초기 서비스는 하나의 `RunService`로 시작한다.

```proto
service RunService {
  rpc SubmitRun(SubmitRunRequest) returns (SubmitRunResponse);
  rpc GetRun(GetRunRequest) returns (GetRunResponse);
  rpc ListRunNodes(ListRunNodesRequest) returns (ListRunNodesResponse);
  rpc CancelRun(CancelRunRequest) returns (CancelRunResponse);
}
```

향후 이벤트 스트리밍이 필요하면 별도 `WatchRun` 또는 `StreamRunEvents`를 추가할 수 있다.
현재 기준선에는 포함하지 않는다.

---

## 4. SubmitRun

### 4.1 역할

- executable run spec 제출
- 기본 validation 수행
- run 등록
- accepted 응답 반환

### 4.2 요청

```proto
message SubmitRunRequest {
  ExecutableRunSpec spec = 1;
  RequestMetadata metadata = 2;
}
```

`RequestMetadata` 예시:

- requester_id
- trace_id
- source_system

### 4.3 응답

```proto
message SubmitRunResponse {
  string run_id = 1;
  RunStatus status = 2;
  string accepted_at = 3;
}
```

### 4.4 의미론

- `SubmitRun` 성공은 등록 성공을 의미한다.
- 실행 완료까지 기다리는 동기 API가 아니다.
- 성공 응답의 기본 상태는 `ACCEPTED`다.
- 이후 dispatch/execution은 비동기로 진행된다.

### 4.5 오류

- validation 실패는 gRPC invalid argument 계열로 반환한다.
- 이미 존재하는 run_id 충돌은 already exists 계열로 반환할 수 있다.
- 내부 registry/backend 초기화 실패는 failed precondition 또는 unavailable 계열로 반환할 수 있다.

---

## 5. GetRun

### 5.1 역할

- run 현재 상태 반환
- run 수준 요약 정보 제공
- 현재 병목 위치와 terminal 종료 원인 제공

### 5.2 요청

```proto
message GetRunRequest {
  string run_id = 1;
}
```

### 5.3 응답

```proto
message GetRunResponse {
  string run_id = 1;
  RunStatus status = 2;
  string accepted_at = 3;
  string started_at = 4;
  string finished_at = 5;
  string current_bottleneck_location = 6;
  string terminal_stop_cause = 7;
  string terminal_failure_reason = 8;
  RunCounters counters = 9;
  map<string, string> metadata = 10;
  OptionalKueueInfo kueue = 11;
}
```

`RunCounters` 예시:

- total_nodes
- succeeded_nodes
- failed_nodes
- canceled_nodes
- skipped_nodes
- running_nodes

### 5.4 의미론

- `status`는 현재 실행 상태를 의미한다.
- `current_bottleneck_location`은 non-terminal 상황에서 유효할 수 있다.
- `terminal_stop_cause`와 `terminal_failure_reason`은 terminal 상황에서만 채워질 수 있다.
- `kueue`는 optional이다.

---

## 6. ListRunNodes

### 6.1 역할

- node별 상태 조회
- node별 attempt 요약 조회
- node별 현재 병목 위치와 terminal 원인 조회

### 6.2 요청

```proto
message ListRunNodesRequest {
  string run_id = 1;
}
```

### 6.3 응답

```proto
message ListRunNodesResponse {
  repeated RunNode nodes = 1;
}

message RunNode {
  string node_id = 1;
  NodeStatus status = 2;
  string current_bottleneck_location = 3;
  string terminal_stop_cause = 4;
  string terminal_failure_reason = 5;
  int32 attempt_count = 6;
  string current_attempt_id = 7;
  string started_at = 8;
  string finished_at = 9;
}
```

### 6.4 의미론

- node 응답은 현재 상태와 terminal 이유를 분리한다.
- attempt 상세 목록은 초기 계약에서 필수로 두지 않는다.
- 필요 시 후속 `GetNodeAttempts` RPC로 분리할 수 있다.

---

## 7. CancelRun

### 7.1 역할

- run 취소 요청 수락
- 아직 시작되지 않은 downstream node 중단
- 이미 시작된 node의 cancel 전파 시도

### 7.2 요청

```proto
message CancelRunRequest {
  string run_id = 1;
  string reason = 2;
}
```

### 7.3 응답

```proto
message CancelRunResponse {
  string run_id = 1;
  bool accepted = 2;
  RunStatus status = 3;
  string accepted_at = 4;
}
```

### 7.4 의미론

- `accepted = true`는 cancel 요청이 수락되었음을 의미한다.
- 실제 backend cancel 완료까지 동기 대기하지 않는다.
- in-flight node는 completion race에 따라 `Succeeded`, `Failed`, `Canceled`가 될 수 있다.
- 최종 run terminal은 별도 `GetRun`으로 확인한다.

---

## 8. 공통 타입 초안

### 8.1 RunStatus

```proto
enum RunStatus {
  RUN_STATUS_UNSPECIFIED = 0;
  RUN_STATUS_ACCEPTED = 1;
  RUN_STATUS_ADMITTED = 2;
  RUN_STATUS_RUNNING = 3;
  RUN_STATUS_SUCCEEDED = 4;
  RUN_STATUS_FAILED = 5;
  RUN_STATUS_CANCELED = 6;
}
```

### 8.2 NodeStatus

```proto
enum NodeStatus {
  NODE_STATUS_UNSPECIFIED = 0;
  NODE_STATUS_PENDING = 1;
  NODE_STATUS_READY = 2;
  NODE_STATUS_RELEASING = 3;
  NODE_STATUS_STARTING = 4;
  NODE_STATUS_RUNNING = 5;
  NODE_STATUS_SUCCEEDED = 6;
  NODE_STATUS_FAILED = 7;
  NODE_STATUS_CANCELED = 8;
  NODE_STATUS_SKIPPED = 9;
}
```

### 8.3 ExecutableRunSpec

초기 draft에서는 별도 proto 파일로 분리한다.
이 문서에서는 필드 방향만 고정한다.

- run_id
- graph
- failure_policy
- metadata
- submitted_at
- nodes
- edges

---

## 9. Optional Kueue 정보

```proto
message OptionalKueueInfo {
  bool observed = 1;
  string queue_name = 2;
  string workload_name = 3;
  string pending_reason = 4;
}
```

원칙:

- 이 필드는 optional이다.
- no-Kueue 환경에서도 `GetRun`과 `ListRunNodes`는 완전해야 한다.
- Kueue 부재는 오류가 아니라 단순 비활성 상태다.

---

## 10. 예시 흐름

### 10.1 submit 후 실행

1. caller가 `SubmitRun` 호출
2. JUMI가 validation 후 `ACCEPTED` 반환
3. caller가 `GetRun`으로 `ADMITTED`, `RUNNING`, `SUCCEEDED/FAILED/CANCELED`를 조회

### 10.2 cancel

1. caller가 `CancelRun` 호출
2. JUMI가 `accepted=true` 응답
3. caller가 `GetRun`과 `ListRunNodes`로 terminal 정리를 확인

---

## 11. 후속 문서

이 문서 다음으로 필요한 것은 아래와 같다.

- Executable Run Spec Draft
- Event / Watch Contract Draft
- HTTP health/readiness/status contract
