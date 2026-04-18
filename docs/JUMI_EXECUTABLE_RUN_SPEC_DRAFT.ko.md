# JUMI Executable Run Spec Draft

> 작성일: 2026-04-18
> 목적: JUMI가 직접 이해하고 실행할 수 있는 Executable Run Spec의 현재 기준 초안을 고정한다.

---

## 1. 문서 목적

이 문서는 JUMI의 northbound 입력 계약인 Executable Run Spec의 최소 구조와 해석 원칙을 정의한다.

이 문서의 목적은 아래와 같다.

- JUMI가 받는 입력이 authored pipeline이 아니라 executable run spec임을 고정한다.
- run, graph, node, policy 필드의 최소 구조를 정의한다.
- validation 규칙과 기본 해석 규칙을 고정한다.
- pipeline lowering, policy scheduler, optional Kueue integration과의 경계를 분리한다.

---

## 2. 기본 원칙

- JUMI는 authored pipeline을 직접 해석하지 않는다.
- JUMI는 lowering이 끝난 executable run spec만 입력으로 받는다.
- Executable Run Spec은 JUMI가 바로 DAG 실행으로 연결할 수 있는 형태여야 한다.
- toolRef resolution, slot resolution, fan-out expansion은 모두 외부 계층 책임이다.
- Kueue 전용 필드는 optional이어야 하며, 없어도 spec은 유효할 수 있어야 한다.
- 직렬화 형식보다 계약 경계가 더 중요하다. 초기 구현은 JSON 또는 proto 어느 쪽으로 시작해도 된다.

---

## 3. 최상위 구조

Executable Run Spec은 아래 구조를 가진다.

```text
ExecutableRunSpec
  - run
  - graph
  - defaults
  - metadata
```

### 3.1 최상위 필드

- `run.runId`
- `run.submittedAt`
- `run.failurePolicy`
- `graph.nodes`
- `graph.edges`
- `defaults`
- `metadata`

---

## 4. Run 공통 필드

### 4.1 필수 필드

- `runId`
- `submittedAt` 또는 등가 제출 시각 정보
- `failurePolicy`

### 4.2 권장 필드

- `requesterId`
- `traceId`
- `sourceSystem`
- `labels`
- `annotations`

### 4.3 의미

- `runId`
  - JUMI 내부에서 run을 식별하는 유일 키다.
- `submittedAt`
  - 외부 계층이 run을 제출한 시각이다.
- `failurePolicy`
  - failure propagation의 기본 동작을 정한다.

### 4.4 예시

```json
{
  "run": {
    "runId": "run-20260418-001",
    "submittedAt": "2026-04-18T09:00:00Z",
    "failurePolicy": {
      "mode": "fail-fast"
    }
  }
}
```

---

## 5. Graph 구조

### 5.1 필수 필드

- `nodes`
- `edges`

### 5.2 의미

- `nodes`
  - 실행 가능한 node 목록이다.
- `edges`
  - node 간 dependency 관계다.

### 5.3 edge 표현

초기 기준선에서는 단순 방향 edge로 시작한다.

```json
{
  "edges": [
    ["a", "b"],
    ["b", "c"]
  ]
}
```

의미:

- `["a", "b"]`는 `a` 완료 후 `b`가 실행 가능함을 뜻한다.

### 5.4 제약

- graph는 DAG여야 한다.
- edge endpoint는 반드시 존재하는 nodeId를 가리켜야 한다.
- self-loop는 허용하지 않는다.

---

## 6. Node 구조

각 node는 JUMI가 backend 실행으로 직접 변환할 수 있는 정보만 포함해야 한다.

### 6.1 필수 필드

- `nodeId`
- `image`
- `command`
- `args`
- `executionClass`
- `resourceProfile`
- `timeoutPolicy`
- `retryPolicy` 또는 retry 없음 명시

### 6.2 선택 필드

- `env`
- `mounts`
- `inputs`
- `outputs`
- `workingDir`
- `serviceAccountName`
- `metadata`
- `kueue`

### 6.3 필드 의미

- `nodeId`
  - run 내부에서 유일해야 하는 node 식별자
- `image`
  - Kubernetes Job/Pod로 매핑 가능한 실행 이미지
- `command`, `args`
  - backend 실행 명령 정의
- `executionClass`
  - backend class 선택을 위한 실행 등급
- `resourceProfile`
  - cpu/memory 등 자원 요청의 기본 단위
- `timeoutPolicy`
  - node 실행 timeout 기준
- `retryPolicy`
  - node 단위 attempt 재시도 허용 여부와 한도
- `inputs`, `outputs`
  - 외부 authored 의미론이 아니라 실행 입출력 힌트
- `kueue`
  - optional Kueue 연동 시 사용할 부가 정보

### 6.4 예시

```json
{
  "nodeId": "b",
  "image": "busybox:1.36",
  "command": ["sh", "-c"],
  "args": ["cat /work/a.txt > /out/b.txt"],
  "executionClass": "standard",
  "resourceProfile": {
    "cpu": "100m",
    "memory": "128Mi"
  },
  "timeoutPolicy": {
    "seconds": 300
  },
  "retryPolicy": {
    "maxAttempts": 1
  },
  "inputs": ["a.txt"],
  "outputs": ["b.txt"]
}
```

---

## 7. Defaults 구조

`defaults`는 run 수준 공통 기본값을 제공한다.

초기 후보 필드:

- `executionClass`
- `resourceProfile`
- `timeoutPolicy`
- `retryPolicy`
- `namespace`

해석 규칙:

- node에 명시된 값이 있으면 node 값이 우선한다.
- node에 값이 없으면 defaults를 상속한다.
- defaults에도 값이 없으면 JUMI의 내부 fallback을 적용할 수 있다.

주의:

- defaults는 authored pipeline의 추상 의미를 담기 위한 필드가 아니다.
- defaults는 실행 계약의 단순 반복 제거 용도다.

---

## 8. Failure Policy Draft

초기 기준선은 단순 failure policy로 시작한다.

```json
{
  "failurePolicy": {
    "mode": "fail-fast"
  }
}
```

현재 허용 후보:

- `fail-fast`
- `continue-if-possible` (후속 확장용, 초기 구현 필수 아님)

해석 규칙:

- 값이 없으면 `fail-fast`를 기본값으로 둔다.
- retry exhaustion 이후의 동작도 이 policy에 따른다.

---

## 9. Retry Policy Draft

retry는 node 단위 제한된 execution semantics다.

### 9.1 최소 필드

- `maxAttempts`

### 9.2 선택 필드

- `retryablePhases`
- `retryDelayHint`

### 9.3 해석 규칙

- 값이 없으면 `no-retry`로 해석한다.
- `maxAttempts = 1`은 실행 1회, 재시도 없음이다.
- 실제 backoff/classification 정책은 외부 계층이 더 풍부하게 계산하더라도, JUMI는 최소 실행 semantics만 사용한다.

### 9.4 예시

```json
{
  "retryPolicy": {
    "maxAttempts": 2
  }
}
```

---

## 10. Timeout Policy Draft

### 10.1 최소 필드

- `seconds`

### 10.2 해석 규칙

- timeout 단위는 명확해야 한다.
- node에 없으면 defaults를 상속한다.
- defaults에도 없으면 JUMI 운영 기본값을 적용할 수 있다.

### 10.3 예시

```json
{
  "timeoutPolicy": {
    "seconds": 300
  }
}
```

---

## 11. Optional Kueue 필드

Kueue 관련 정보는 optional extension이어야 한다.

초기 후보:

- `queueName`
- `workloadClass`
- `labels`

예시:

```json
{
  "kueue": {
    "queueName": "standard",
    "workloadClass": "batch"
  }
}
```

원칙:

- 이 필드가 없어도 spec은 유효해야 한다.
- no-Kueue 환경에서도 동일 spec이 실행 가능해야 한다.
- Kueue 필드는 core execution semantics를 바꾸는 필수 조건이 아니다.

---

## 12. Validation 규칙

JUMI는 `SubmitRun` 시 최소한 아래를 검증해야 한다.

- `runId` 존재 여부
- `runId` 중복 여부
- `nodes` 비어 있지 않음
- `nodeId` 유일성
- edge endpoint 유효성
- graph acyclic 보장
- 필수 실행 필드 존재 여부
- timeout 단위 유효성
- retryPolicy 값 유효성
- failurePolicy 값 유효성

Validation 실패는 등록 실패다.
즉 `Accepted` 상태를 만들지 않는다.

---

## 13. JSON 예시

```json
{
  "run": {
    "runId": "run-20260418-001",
    "submittedAt": "2026-04-18T09:00:00Z",
    "failurePolicy": {
      "mode": "fail-fast"
    }
  },
  "defaults": {
    "executionClass": "standard",
    "resourceProfile": {
      "cpu": "100m",
      "memory": "128Mi"
    },
    "timeoutPolicy": {
      "seconds": 300
    },
    "retryPolicy": {
      "maxAttempts": 1
    }
  },
  "metadata": {
    "requesterId": "scheduler-a",
    "traceId": "trace-001"
  },
  "graph": {
    "nodes": [
      {
        "nodeId": "a",
        "image": "busybox:1.36",
        "command": ["sh", "-c"],
        "args": ["echo prepare > /work/a.txt"],
        "outputs": ["a.txt"]
      },
      {
        "nodeId": "b",
        "image": "busybox:1.36",
        "command": ["sh", "-c"],
        "args": ["cat /work/a.txt > /out/b.txt"],
        "inputs": ["a.txt"],
        "outputs": ["b.txt"],
        "kueue": {
          "queueName": "standard"
        }
      },
      {
        "nodeId": "c",
        "image": "busybox:1.36",
        "command": ["sh", "-c"],
        "args": ["cat /out/b.txt"],
        "inputs": ["b.txt"]
      }
    ],
    "edges": [
      ["a", "b"],
      ["b", "c"]
    ]
  }
}
```

---

## 14. 외부 계층과의 경계

### 14.1 Pipeline Lowering

JUMI는 아래를 spec 안에서 더 이상 해석하지 않는다.

- authored pipeline 단계 이름
- template expansion
- fan-out expansion
- binding resolution
- slot allocation

이런 작업은 lowering이 끝난 뒤에만 JUMI에 들어온다.

### 14.2 Policy Scheduler

JUMI는 아래를 spec 밖에서 결정된 것으로 본다.

- 어떤 run을 지금 제출할지
- 어떤 queue/priority를 적용할지
- 복잡한 retry class를 어떻게 계산할지
- multi-tenant fairness를 어떻게 보장할지

### 14.3 Redis Streams

Redis Streams는 JUMI 입력 계약이 아니다.
JUMI는 Redis 메시지를 직접 읽는 consumer가 아니라, 실행 명세를 받는 execution app이다.

---

## 15. 후속 문서

이 문서 다음으로 연결될 것은 아래와 같다.

- JUMI gRPC Contract Draft
- proto schema draft
- sample fixture catalog
