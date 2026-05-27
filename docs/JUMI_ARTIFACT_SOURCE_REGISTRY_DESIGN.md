# Artifact Source Registry / Materialization Source Layer

> 작성일: 2026-05-25  
> 상태: Confirmed Design  
> 목적: `logicalUri`와 concrete fetch/materialization source를 분리하는 장기 구조를 고정한다.

---

## 1. 한 줄 요약

Sprint 3A와 Sprint 3B를 통해 다음 두 happy path는 닫혔다.

- `remote_fetch` via pre-seeded `simple_http`
- `local_reuse` via pure K8s same-node node-local CAS

하지만 두 경로 모두 장기적으로는 같은 문제를 남긴다.

```text
artifact identity 는 무엇인가?
artifact content 는 무엇인가?
현재 이 content 를 실제로 어디서 가져올 수 있는가?
```

이 문서는 그 셋을 분리하기 위해 `Artifact Source Registry / Materialization Source Layer`를 도입하는 장기 방향을 정리한다.

---

## 2. 왜 필요한가

현재도 다음은 이미 분리되기 시작했다.

- `logicalUri`
- `producerAttemptId`
- `digest`
- `locations[]`

하지만 아직은 다음이 부족하다.

- source 추가/삭제 lifecycle
- 동일 artifact의 multi-location 관리
- transport capability에 따른 source selection
- scheduling 결과와 materialization 후보의 연결
- source health / reachability / freshness 관리

즉 지금은 registry 없는 single-write metadata 모델이고, 다음 단계는 location-aware source registry 모델이다.

---

## 3. 목표

Artifact Source Registry의 목표는 다음이다.

1. artifact identity를 stable하게 유지한다.
2. content identity를 digest로 고정한다.
3. concrete source는 backend별 typed location으로 관리한다.
4. source는 시간이 지나며 추가될 수 있다.
5. ResolveBinding은 source 후보와 placement 강도를 함께 계산할 수 있어야 한다.

비목표:

- Sprint 3A / 3B happy path 자체를 다시 구현하는 것
- storage backend를 한 번에 모두 붙이는 것
- 즉시 production-grade distributed cache를 완성하는 것

---

## 4. 핵심 개체

### 4.1 Logical Artifact

run-scoped output slot identity.

예:

```text
jumi://runs/{runId}/nodes/{nodeId}/outputs/{outputName}
```

의미:

- "무엇"인가를 나타낸다.
- fetch 가능한 주소를 의미하지 않는다.

### 4.2 Producer Attempt

실제로 그 artifact를 생성한 execution attempt.

예:

```text
producerAttemptId = attempt-2
```

의미:

- 동일 logical slot이라도 retry마다 달라진다.
- provenance와 failure analysis에 필요하다.

### 4.3 Content Identity

digest 기반 content identity.

예:

```text
sha256:abc123...
```

의미:

- backend가 달라도 같은 digest면 같은 content다.

### 4.4 Source Location

실제로 content에 도달 가능한 source.

예:

- node_local
- http
- object_store
- dragonfly
- peer
- external_command

---

## 5. 제안 모델

### 5.1 Artifact Record

```yaml
artifact:
  logicalUri: jumi://runs/run-1/nodes/A/outputs/result
  producerAttemptId: attempt-2
  digest: sha256:abc123...
  sizeBytes: 1048576
  sources:
    - sourceId: src-1
      backend: node_local
      location:
        nodeName: worker-2
        path: /var/lib/jumi-artifacts/cas/sha256/abc123...
      state: ready
    - sourceId: src-2
      backend: http
      location:
        uri: http://artifact-source.local/artifacts/abc123
      state: ready
```

원칙:

- `logicalUri`는 slot identity
- `producerAttemptId`는 execution identity
- `digest`는 content identity
- `sources[]`는 concrete materialization source

### 5.2 Source State

source는 항상 usable한 것이 아니다.

최소 state:

- `ready`
- `pending`
- `deleted`
- `stale`
- `unreachable`

장기적으로는 metrics도 붙을 수 있다.

- `lastVerifiedAt`
- `lastError`
- `backendLatencyClass`
- `costHint`

---

## 6. Backend Union

장기적으로는 flat struct보다 typed union이 맞다.

예:

```protobuf
message SourceLocation {
  oneof backend {
    NodeLocalSource node_local = 1;
    HttpSource http = 2;
    ObjectStoreSource object_store = 3;
    DragonflySource dragonfly = 4;
    PeerSource peer = 5;
    ExternalCommandSource external_command = 6;
  }
}
```

Sprint 3A/3B에서 실제로 검증한 것은 아래 둘뿐이다.

- `node_local`
- `http`

---

## 7. ResolveBinding 장기 방향

현재 happy path는 single selected plan으로 충분했다.

장기적으로는 ordered candidates가 필요하다.

예:

```yaml
placement:
  requiredNodeName: worker-2
  preferredNodeNames:
    - worker-2
materializationCandidates:
  - priority: 1
    mode: local_reuse
    sourceRef: src-node-local
    condition: scheduledOn=worker-2
  - priority: 2
    mode: dragonfly_pull
    sourceRef: src-dragonfly
    condition: dragonfly_available
  - priority: 3
    mode: remote_fetch
    sourceRef: src-http
```

이 구조가 필요한 이유:

- scheduling actual node와 source availability가 달라질 수 있다.
- fallback을 정식으로 표현해야 한다.
- placement 강도는 available transport에 따라 달라져야 한다.

---

## 8. Source 추가 lifecycle

Artifact Source Registry가 있으면 source는 시간이 지나며 추가될 수 있다.

예:

1. producer 종료 직후
   - `node_local` source만 존재
2. 비동기 publish 완료 후
   - `http` 또는 `object_store` source 추가
3. peer advertisement 완료 후
   - `peer` source 추가

후속 API 예:

- `AddSource(artifactId, source)`
- `UpdateSourceState(sourceId, state)`
- `ListSources(artifactId)`

즉 Sprint 3B의 `locations[] initial write`는 registry 모델의 시작점일 뿐이다.

---

## 9. JUMI / AH / nan 책임 분리

### AH / Registry layer

- artifact identity 관리
- source 목록 관리
- source state 관리
- ResolveBinding candidate 선택

### JUMI

- AH가 준 placement/materialization contract 전달
- actual scheduling result 관찰
- 필요 시 post-scheduling resolve 재호출

### nan / runtime helper

- 선택된 plan을 실행
- digest 검증
- promotion/materialization 수행

원칙:

JUMI는 source registry가 아니다.  
JUMI는 source selection orchestrator도 아니다.  
그 책임의 정본은 AH 뒤의 registry/materialization layer다.

---

## 10. Sprint 3A / 3B와의 관계

### Sprint 3A에서 확인한 것

- fetchable `http` source가 있으면 consumer가 `remote_fetch` 가능

### Sprint 3B에서 확인한 것

- same worker node라면 producer-side node-local promotion 후 consumer가 `local_reuse` 가능

즉 registry 설계는 이미 검증된 두 source type을 하나의 모델로 합치는 단계다.

---

## 11. Sprint 3C 분할

Sprint 3C는 한 번에 구현하지 않고 두 단계로 나눈다.

### 11.1 Sprint 3C-1

목표:

- `ArtifactRecord`와 `ArtifactSourceRecord`를 분리한다.
- `node_local`과 `http`를 같은 source record 모델로 저장한다.
- `RegisterArtifact -> initialSources` 경로를 도입한다.
- `ListSources`를 추가한다.
- 기존 `ResolveBinding` 외부 응답 shape는 최대한 유지한다.

즉 3C-1은 저장 모델과 backward compatibility를 먼저 닫는 단계다.

### 11.2 Sprint 3C-2

목표:

- `ResolveBinding` 내부 planner를 `materializationCandidates[]` 기반으로 올린다.
- `node_local > http` 우선순위를 source-aware planner로 계산한다.
- placement strength를 source 조합과 target context에 따라 계산한다.
- legacy single-plan adapter는 유지한다.

즉 3C-2는 source selection / candidate planner를 도입하는 단계다.

## 12. 다음 구현 후보

우선순위는 이 순서가 맞다.

1. 3C-1: AH storage 모델에서 source typed payload를 정식화
2. 3C-1: `RegisterArtifact -> initialSources`와 `ListSources` 추가
3. 3C-2: ResolveBinding candidate response shape 도입
4. 3C-2: source-aware placement / materialization planner 연결
5. post-scheduling `ResolveBinding(targetNodeName=actualNode)` 연결

---

## 13. 현재 결론

현재 happy path 상태:

- Sprint 3A: 완료
- Sprint 3B: 완료

다음 단계의 핵심은 transport backend를 더 붙이는 것이 아니라,
검증된 `node_local`과 `http` source를 **하나의 source registry 모델로 승격**하는 것이다.

한 줄 결론:

Artifact Source Registry / Materialization Source Layer는  
`logicalUri`, `producerAttemptId`, `digest`, `sources[]`를 분리해서  
ResolveBinding이 placement와 materialization 후보를 함께 계산할 수 있게 만드는 다음 단계의 정본 계층이다.
