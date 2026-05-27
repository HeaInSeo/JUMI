# Sprint 3B Design

> 작성일: 2026-05-23  
> 상태: Draft / Preflight Required  
> 목적: pure K8s 환경에서 same-node `local_reuse` handoff와 producer-side node-local artifact promotion happy path를 고정한다.

## 1. 문서 목적

Sprint 3A에서는 consumer `remote_fetch` materialization을 검증했다.

즉, 이미 준비된 HTTP source에 artifact가 있고, B DAG node가 그 artifact를 가져와서 digest 검증 후 `/work/inputs/<inputName>`에 준비하는 흐름을 확인했다.

Sprint 3A는 producer 쪽 검증이 아니라 consumer materialization 검증이었다.

Sprint 3B에서는 방향을 다음과 같이 재정의한다.

Sprint 3B는 pure K8s 환경에서 same-node `local_reuse` handoff와 producer-side node-local artifact promotion을 검증한다.

즉, Sprint 3B는 단순한 upload path 검증이 아니다.

더 정확한 목표는 다음과 같다.

A DAG node가 만든 artifact를 같은 Kubernetes worker node의 node-local artifact area로 승격시키고, B DAG node가 같은 worker node에서 그 artifact를 `local_reuse`로 사용할 수 있는지 검증한다.

## 2. Sprint 3B 핵심 요약

Sprint 3B의 happy path는 다음과 같다.

```text
A Pod on worker-2
  ↓
A가 /out/result.bam 생성
  ↓
producer-side promotion
  ↓
worker-2 node-local artifact area에 copy
  ↓
manifest에 logicalUri + producerAttemptId + digest + node_local location 기록
  ↓
JUMI가 AH에 RegisterArtifact
  ↓
AH가 worker-2 node_local location 기억
  ↓
B 실행 전 AH ResolveBinding
  ↓
B도 worker-2에 배치
  ↓
B가 node-local artifact를 local_reuse
  ↓
digest 검증
  ↓
/work/inputs/result.bam 으로 copy materialization
  ↓
B command 실행 성공
```

한 줄로 정리하면:

Sprint 3B는 external upload가 아니라, pure K8s same-node local handoff를 검증하는 단계다.

## 3. Sprint 3A와 Sprint 3B 차이

### 3.1 Sprint 3A

Sprint 3A는 consumer 쪽 검증이었다.

```text
pre-seeded simple_http source
  ↓
B가 remote_fetch
  ↓
sha256 digest 검증
  ↓
/work/inputs/<inputName> materialization
  ↓
B 성공
```

Sprint 3A에서 확인한 것:

- `remote_fetch` 동작
- sha256 검증
- `/work/inputs` materialization
- `1MiB / 10MiB / 100MiB` consumer materialization smoke

Sprint 3A에서 아직 검증하지 않은 것:

- A가 만든 artifact를 실제 handoff 가능한 위치로 승격하는 과정
- same-node `local_reuse`
- node-local location model
- producer-side promotion

### 3.2 Sprint 3B

Sprint 3B는 producer-side handoff와 same-node local reuse를 검증한다.

```text
A가 artifact 생성
  ↓
producer-side node-local artifact promotion
  ↓
node-local artifact area에 copy
  ↓
AH가 node_local location 기억
  ↓
B가 같은 worker node에서 local_reuse
```

즉 Sprint 3B의 핵심은 다음 두 가지다.

1. producer-side node-local artifact promotion
2. same-node `local_reuse` materialization

## 4. 핵심 용어

### 4.1 DAG Node

DAG상의 작업 단위다.

예:

```text
A -> B
```

여기서 A와 B는 DAG node다.

### 4.2 Kubernetes Worker Node

Pod/Job이 실제로 실행되는 Kubernetes의 물리 또는 가상 worker node다.

예:

```text
worker-1
worker-2
worker-3
```

주의할 점:

DAG node B와 Kubernetes worker node는 다르다.

이 문서에서는 다음처럼 구분한다.

```text
DAG node:
  A, B, C

Kubernetes worker node:
  worker-1, worker-2, worker-3
```

### 4.3 Artifact

DAG node가 생성한 결과 파일이다.

예:

```text
result.bam
result.vcf
metrics.json
```

### 4.4 Node-local Artifact Area

특정 Kubernetes worker node에 존재하는 artifact 저장 위치다.

예:

```text
/var/lib/jumi-artifacts/cas/sha256/<digest>
```

Pod 내부에서는 다음과 같은 경로로 mount될 수 있다.

```text
/jumi-node-artifacts/cas/sha256/<digest>
```

이 위치는 같은 worker node에서 실행되는 여러 Pod/Job이 공유해서 접근할 수 있다.

단, 이것은 node-local backend의 한 location일 뿐이다. CAS 자체가 node-local storage를 의미하는 것은 아니다.

### 4.5 local_reuse

Sprint 3B에서 `local_reuse`는 다음 의미로 고정한다.

```text
local_reuse =
same Kubernetes worker node에서
node-local artifact area에 이미 존재하는 artifact를
네트워크 전송 없이 재사용하는 materialization mode
```

`local_reuse`의 조건은 다음과 같다.

- producer Pod와 consumer Pod가 같은 Kubernetes worker node에 있다.
- artifact가 node-local artifact area에 존재한다.
- 네트워크 전송 없이 접근한다.
- expected digest와 실제 digest를 검증한다.
- consumer의 `/work/inputs/<inputName>`으로 copy materialize한다.

즉 `local_reuse`는 단순히 “로컬 파일을 읽는다”가 아니다.

정확히는:

```text
same worker node
+ node-local artifact area
+ no network transfer
+ digest verification
+ copy into /work/inputs materialization
```

을 만족하는 materialization mode다.

### 4.6 remote_fetch

`remote_fetch`는 artifact가 현재 consumer Pod가 실행되는 worker node에 없거나, fetchable URI를 통해 가져와야 할 때 사용하는 materialization mode다.

예:

```text
http://...
s3://...
peer://...
dragonfly-backed source
```

Sprint 3A에서 검증한 것은 이쪽에 가깝다.

Sprint 3B에서는 `remote_fetch` fallback을 검증하지 않는다.

### 4.7 producer-side node-local artifact promotion

Sprint 3B의 핵심 구현 포인트다.

정의:

```text
producer-side promotion =
A Pod가 생성한 output artifact를
node-local artifact area의 CAS 경로로 copy하고,
digest와 node_local location을 output manifest에 기록하는 과정
```

Sprint 3B v0에서는 promotion 동작을 `copy into CAS path`로 고정한다.

```text
/out/result.bam
  ↓ copy
/jumi-node-artifacts/cas/sha256/<digest>
  ↓
manifest.locations[]에 기록
```

여기서 `/out/result.bam`은 producer의 원본 output으로 남겨둔다.

```text
/out/result.bam
  = producer 원본 output
  = 디버깅 / 실패 분석 / 재시도 분석에 사용 가능

/jumi-node-artifacts/cas/sha256/<digest>
  = handoff용 승격 artifact
  = consumer local_reuse 대상
```

v0에서 move가 아니라 copy를 사용하는 이유는 다음과 같다.

- producer 원본 output을 보존할 수 있다.
- 실패 분석이 쉬워진다.
- promoted CAS artifact와 raw output을 분리할 수 있다.
- 재시도/검증 로직을 단순하게 만들 수 있다.

## 5. Identity 모델

Sprint 3B에서는 artifact identity를 세 계층으로 분리한다.

1. logical identity
2. producer execution metadata
3. content identity

### 5.1 logicalUri

`logicalUri`는 artifact의 run-scoped logical slot identity를 나타낸다.

Sprint 3B에서는 `logicalUri`에 `attemptId`를 넣지 않는다.

권장 형식:

```text
jumi://runs/{runId}/nodes/{nodeId}/outputs/{outputName}
```

예:

```text
jumi://runs/run-1/nodes/A/outputs/result
```

`attemptId`를 `logicalUri`에 넣지 않는 이유:

- retry 시 `attemptId`가 바뀌어도 logical slot은 동일하게 유지된다.
- `logicalUri`가 execution attempt identity와 섞이지 않는다.
- digest 기반 content identity와 logical identity를 분리할 수 있다.
- provenance 추적 시 attempt 변경과 artifact slot identity를 구분할 수 있다.

### 5.2 logicalUri opaque 처리 원칙

Sprint 3B에서는 `logicalUri` scheme을 당장 멀티 테넌트/멀티 클러스터 구조로 확장하지 않는다.

v0 형식:

```text
jumi://runs/{runId}/nodes/{nodeId}/outputs/{outputName}
```

후속 확장 가능성:

```text
jumi://{orgId}/{clusterId}/runs/{runId}/nodes/{nodeId}/outputs/{outputName}
```

따라서 v0 코드에서는 `logicalUri`를 일반 문자열로 직접 파싱하지 않는다.

원칙:

```text
logicalUri는 opaque string으로 취급한다.
필요한 경우 전용 parser/formatter 함수를 통해서만 다룬다.
```

### 5.3 producerAttemptId

`producerAttemptId`는 artifact를 실제로 생성한 execution attempt를 나타낸다.

예:

```json
{
  "logicalUri": "jumi://runs/run-1/nodes/A/outputs/result",
  "producerAttemptId": "attempt-2"
}
```

즉:

```text
logicalUri:
  이 run에서 A node의 result output slot

producerAttemptId:
  그 slot을 실제로 생성한 attempt

digest:
  실제 content identity
```

### 5.4 contentDigest

`digest`는 content identity다.

예:

```text
sha256:abc123...
```

같은 digest는 같은 content를 의미한다.

단, Sprint 3B에서 cross-run deduplication이나 same-digest search API를 구현하지 않는다.

## 6. CAS identity와 location 분리

Sprint 3B에서는 CAS를 node-local storage가 아니라 content-addressed identity로 본다.

```text
CAS identity = sha256:<digest>
```

이 content identity는 여러 location에 존재할 수 있다.

예:

```text
sha256:abc123
  - node_local:
      nodeName: worker-2
      path: /var/lib/jumi-artifacts/cas/sha256/abc123

  - object_store:
      bucket: jumi-artifacts
      key: cas/sha256/abc123

  - dragonfly:
      tag: sha256:abc123
      seedPeers: [...]

  - http:
      uri: http://...
```

따라서 다음 원칙을 고정한다.

```text
CAS는 content identity다.
node-local CAS path는 그 content identity가 node-local backend에 저장된 하나의 location일 뿐이다.
```

## 7. Location 모델

Sprint 3B에서는 `locations[]`를 flat struct로 보지 않는다.

장기적으로는 type별 payload가 다른 sealed union 또는 protobuf `oneof` 모델을 지향한다.

개념 예시:

```proto
message Location {
  oneof backend {
    NodeLocalLocation node_local = 1;
    ObjectStoreLocation object_store = 2;
    DragonflyLocation dragonfly = 3;
    PeerLocation peer = 4;
    HttpLocation http = 5;
  }
}
```

Sprint 3B에서는 `NodeLocalLocation`만 구현한다.

```text
Sprint 3B scope:
  Location.oneof.node_local only
```

후속 확장:

```text
future:
  object_store
  dragonfly
  peer
  http
  external_command
```

## 8. ResolveBinding 모델

### 8.1 Sprint 3B v0 실행 범위

Sprint 3B v0에서는 하나의 materialization plan만 실제 사용한다.

```text
selected plan:
  mode = local_reuse
  sourceLocation = node_local(worker-2)
```

하지만 장기적으로는 단일 plan 구조가 아니라 ordered candidates 구조를 지향한다.

### 8.2 Future ResolveBinding response shape

후속 설계에서 ResolveBinding 응답은 ordered candidates를 가질 수 있어야 한다.

Sprint 3B에서는 이를 구현하지 않고, 아래 원칙만 예약한다.

```text
Sprint 3B:
  materializationCandidates[0] = local_reuse only

Future:
  ResolveBinding may return ordered materialization candidates.
```

### 8.3 Placement와 materialization의 관계

Sprint 3B에서는 `node_local only` 환경을 가정한다.

따라서 B는 A와 같은 worker node에 떠야 한다.

```text
node_local only:
  placement.requiredNodeName = worker-2
  materialization.mode = local_reuse
```

### 8.4 Future ResolveBinding request placeholder

후속 ResolveBinding 요청에는 환경의 transport capability가 들어갈 수 있다.

Sprint 3B에서는 다음을 가정한다.

```text
availableTransports = ["node_local"]
```

즉 Sprint 3B는 transport-aware placement planner가 아니라, node-local happy path 검증이다.

## 9. nan Materializer interface 경계

Sprint 3B에서는 nan/runtime helper가 `local_reuse`를 직접 수행할 수 있다.

하지만 transport별 코드가 nan 내부에 무한히 쌓이는 것은 피해야 한다.

따라서 nan 내부에는 최소한 다음 경계를 둔다.

```go
type Materializer interface {
    Materialize(ctx context.Context, plan MaterializationPlan) error
}
```

Sprint 3B 구현 범위:

```go
type LocalReuseMaterializer struct{}
```

후속 확장 예:

```go
type RemoteFetchMaterializer struct{}
type DragonflyMaterializer struct{}
type PeerFetchMaterializer struct{}
type ObjectStoreMaterializer struct{}
```

Sprint 3B에서는 `LocalReuseMaterializer`만 구현한다.

## 10. Sprint 3B의 설계 원칙

Sprint 3B는 다음 원칙을 따른다.

1. pure K8s happy path를 기준으로 한다.
2. 외부 object storage를 기본 전제로 두지 않는다.
3. S3/MinIO, Dragonfly, peer_fetch는 이번 범위에서 제외한다.
4. PV/PVC 중심 공유 저장소를 기본 경로로 삼지 않는다.
5. 같은 worker node에서의 data locality를 먼저 검증한다.
6. JUMI는 대용량 파일 전송 책임을 갖지 않는다.
7. AH는 artifact identity와 location을 구분해서 관리한다.
8. nan/runtime helper는 producer-side promotion과 consumer-side materialization의 실행 주체가 될 수 있다.
9. v0 producer-side promotion은 `copy into CAS path`로 고정한다.
10. v0 consumer-side `local_reuse`도 `copy into /work/inputs`로 고정한다.
11. CAS path는 content-addressed location이므로 임의 overwrite하지 않는다.
12. CAS는 node-local storage가 아니라 content identity다.
13. Location은 장기적으로 `oneof`/union 모델을 지향한다.
14. ResolveBinding은 장기적으로 ordered candidates 구조를 지향한다.
15. Sprint 3B는 `node_local only`, single `local_reuse` candidate만 사용한다.
16. promotion 또는 manifest 작성 실패는 producer DAG node 실패로 처리한다.

## 11. hostPath 사용에 대한 명확한 입장

Sprint 3B v0에서는 pure K8s node-local handoff를 검증하기 위해 `hostPath`를 사용할 수 있다.

하지만 이것은 검증용 baseline이다.

```text
Sprint 3B v0 uses hostPath only as a validation mechanism.
hostPath is not yet a production default recommendation.
```

한국어로 정리하면:

Sprint 3B v0에서 `hostPath`는 검증용 메커니즘일 뿐이다. 아직 production default 권장안은 아니다.

## 12. Node-local Artifact Area 구조

Sprint 3B v0에서는 다음과 같은 node-local artifact area를 가정한다.

worker node 실제 경로:

```text
/var/lib/jumi-artifacts
```

Pod 내부 mount 경로:

```text
/jumi-node-artifacts
```

CAS 구조:

```text
/var/lib/jumi-artifacts/
  cas/
    sha256/
      abc123...
      def456...
  tmp/
    ...
```

중요한 점:

`/var/lib/jumi-artifacts/cas/sha256/<digest>`는 node-local backend에서의 location path일 뿐이다. CAS identity 자체는 `sha256:<digest>`다.

## 13. CAS overwrite / idempotency 정책

Sprint 3B v0에서는 CAS 경로를 content-addressed location으로 본다.

v0 정책은 다음과 같이 둔다.

1. CAS path가 아직 없으면 새로 생성한다.
2. CAS path가 이미 있고 digest가 같으면 검증 후 재사용한다.
3. CAS path가 이미 있는데 expected digest와 실제 content가 다르면 실패한다.
4. 기존 CAS artifact를 임의 overwrite하지 않는다.
5. copy 중에는 임시 경로를 사용하고, digest 검증 후 최종 CAS 경로로 rename한다.

권장 흐름:

```text
/out/result.bam
  ↓ copy to temp
/jumi-node-artifacts/tmp/<runId>-<nodeId>-<attemptId>-<outputName>-<nonce>
  ↓ sha256 verify
/jumi-node-artifacts/cas/sha256/<digest>
```

동일 digest artifact가 이미 존재하는 경우:

```text
existing CAS file
  ↓
digest verify
  ↓
valid이면 reuse
```

digest 불일치 또는 손상된 CAS 파일이 발견된 경우:

```text
existing CAS file
  ↓
digest mismatch
  ↓
promotion failure
  ↓
A DAG node failure
```

## 14. Producer / Consumer Mount 정책

### 14.1 Producer Pod

A Pod는 artifact를 node-local area에 승격해야 하므로 RW mount가 필요하다.

```text
A Pod:
  /jumi-node-artifacts  RW
  /out                  RW
```

### 14.2 Consumer Pod

B Pod는 artifact를 읽기만 하면 된다.

가능하면 node-local artifact area는 RO mount로 둔다.

```text
B Pod:
  /jumi-node-artifacts  RO
  /work                 RW
```

Sprint 3B v0에서 consumer materialization은 copy로 고정한다.

```text
/jumi-node-artifacts/cas/sha256/<digest>
  ↓ copy
/work/inputs/<inputName>
```

v0에서 symlink/hardlink를 사용하지 않는 이유:

- symlink: B command가 input을 수정하면 CAS artifact가 오염될 수 있다.
- hardlink: 같은 filesystem 조건이 필요하고 cross-fs에서 실패할 수 있다.
- copy: 디스크 사용량은 늘지만 가장 안전하고 예측 가능하다.

### 14.3 BUG-2 재분류 메모

`local_reuse`가 `/work/inputs`로 full copy materialization을 수행하는 현재 구현은
Sprint 3B v0 기준에서는 버그로 보지 않는다.

즉 다음 정책은 현재 설계와 일치한다.

```text
node-local CAS artifact
  ↓ copy
/work/inputs/<inputName>
```

이 정책을 유지하는 이유:

- consumer command가 input을 수정해도 CAS 원본이 오염되지 않는다
- same-filesystem, cross-filesystem, permission 차이를 단순하게 다룰 수 있다
- `copy into /work/inputs` 경로가 가장 예측 가능하고 디버깅이 쉽다

재분류:

```text
BUG-2 ❌
LocalReuseMaterializer strategy optimization debt ✅
```

후속 설계 방향:

```text
default:
  copy

optional optimization:
  reflink / copy-on-write clone

explicit opt-in:
  hardlink

fallback:
  copy
```

즉 `hardlink-first`는 즉시 버그 수정이 아니라 후속 materializer strategy 설계 항목으로 남긴다.

## 15. Manifest / RegisterArtifact seam 정책

Sprint 3B 설계에서는 `logicalUri`, `producerAttemptId`, `digest`, `locations[]`를 분리한다.

예시:

```json
{
  "outputs": [
    {
      "name": "result",
      "digest": "sha256:abc123...",
      "sizeBytes": 1048576,
      "logicalUri": "jumi://runs/run-1/nodes/A/outputs/result",
      "producerAttemptId": "attempt-2",
      "locations": [
        {
          "nodeLocal": {
            "nodeName": "worker-2",
            "path": "/var/lib/jumi-artifacts/cas/sha256/abc123..."
          }
        }
      ],
      "provenance": {
        "inputs": []
      }
    }
  ]
}
```

### 15.1 Sprint 3B preflight requirement

Sprint 3B 시작 전에 현재 manifest/RegisterArtifact seam이 아래 정보를 분리해서 전달할 수 있는지 확인한다.

- `logicalUri`
- `producerAttemptId`
- `digest`
- `sizeBytes`
- `locations[]`

현재 JUMI preflight 점검 결과:

- `producerAttemptId`: 부분 통과
- `logicalUri`: 미통과
- `locations[]`: 미통과
- `node_local only` artifact 등록: 미통과

현행 코드 갭:

- output manifest는 per-output record에 `uri`, `digest`, `sizeBytes`만 가진다.
- `backend.OutputMetadata`도 `URI`, `Digest`, `SizeBytes`, `NodeName`, `PodName`만 보존한다.
- `handoff.RegisterArtifactRequest`는 단일 `URI`와 `NodeName`만 전달한다.
- executor는 output 등록 전 `metadata.URI` non-empty를 강제한다.

즉 Sprint 3B를 시작하려면 아래 선행 작업이 필요하다.

```text
manifest/RegisterArtifact seam 보강
```

이 보강의 목적은 JUMI가 대용량 데이터 전송 책임을 갖게 하려는 것이 아니라, 다음 두 정보를 분리해서 전달하기 위함이다.

```text
logical identity:
  이 artifact가 무엇인가?

concrete location:
  이 artifact가 실제로 어디에 있는가?
```

### 15.2 AddLocation / UpdateLocations placeholder

Sprint 3B v0에서는 initial `locations[]`를 한 번 등록한다.

후속 단계에서는 다음 API가 필요할 수 있다.

```text
future AH API:
  AddLocation(artifactId, location)

future AH API:
  UpdateLocations(artifactId, locations[])
```

### 15.3 provenance slot

Sprint 3B에서는 output manifest에 provenance 슬롯을 확보한다.

예:

```json
{
  "provenance": {
    "inputs": [
      {
        "inputName": "result",
        "artifactDigest": "sha256:abc123...",
        "producerLogicalUri": "jumi://runs/run-1/nodes/A/outputs/result"
      }
    ]
  }
}
```

Sprint 3B v0에서 provenance 추적 기능을 완성하지 않아도 된다.

## 16. 전체 Happy Path 상세 흐름

### 16.1 A 실행

```text
JUMI가 A Pod/Job 실행 요청
  ↓
A Pod가 worker-2에서 실행됨
  ↓
A command가 /out/result.bam 생성
```

### 16.2 Producer-side Promotion

```text
nan/runtime helper가 /out/result.bam 확인
  ↓
sha256 digest 계산
  ↓
CAS 경로 결정
  ↓
nonce 포함 tmp 경로로 copy
  ↓
copy된 파일 digest 검증
  ↓
최종 CAS 경로로 rename 또는 기존 CAS artifact reuse
  ↓
sizeBytes 계산
  ↓
output manifest 작성
```

### 16.3 Promotion failure policy

A command가 성공하더라도 promotion 또는 manifest 작성이 실패하면 A DAG node는 실패로 처리한다.

원칙:

```text
A command exit 0만으로 A node 성공으로 보지 않는다.
producer-side promotion과 manifest 작성까지 성공해야 A node 성공이다.
```

### 16.4 Output Manifest

A 완료 후 output manifest에는 다음 정보가 들어간다.

- `logicalUri`
- `producerAttemptId`
- `digest`
- `locations[]`
- `provenance`

### 16.5 JUMI RegisterArtifact

JUMI는 A output manifest를 회수하고 AH에 artifact를 등록한다.

등록해야 할 정보:

- `runId`
- `producer DAG nodeId`
- `producer attemptId`
- `output name`
- `digest`
- `sizeBytes`
- `logicalUri`
- `locations[]`
- `provenance slot`, optional

Sprint 3B 시작 전 이 seam이 가능한지 확인한다. 불가능하면 seam 보강을 Sprint 3B 선행 작업으로 처리한다.

### 16.6 AH ResolveBinding

B가 실행되기 전에 JUMI는 AH에 ResolveBinding을 요청한다.

Sprint 3B v0 응답 개념:

```json
{
  "placement": {
    "requiredNodeName": "worker-2"
  },
  "materializationCandidates": [
    {
      "priority": 1,
      "mode": "local_reuse",
      "sourceLocation": {
        "nodeLocal": {
          "nodeName": "worker-2",
          "path": "/var/lib/jumi-artifacts/cas/sha256/abc123..."
        }
      },
      "expectedDigest": "sha256:abc123...",
      "localPath": "/work/inputs/result"
    }
  ]
}
```

Sprint 3B에서는 `materializationCandidates[0]`만 사용한다.

### 16.7 B Pod 생성

Sprint 3B happy path에서는 결정론이 중요하므로 `requiredNodeName`을 기본으로 둔다.

```text
requiredNodeName = worker-2
```

### 16.8 requiredNodeName test precondition

Sprint 3B 테스트는 다음 조건을 전제로 한다.

- `worker-2`가 Ready 상태여야 한다.
- `worker-2`가 cordon/drain 상태가 아니어야 한다.
- B Pod resource request를 수용할 수 있어야 한다.
- taint/toleration 불일치가 없어야 한다.
- node-local artifact area mount가 가능해야 한다.

### 16.9 B local_reuse materialization

B Pod 내부에는 다음 정보가 env 또는 contract file로 들어간다.

```text
JUMI_INPUT_RESULT_MATERIALIZATION_MODE=local_reuse
JUMI_INPUT_RESULT_EXPECTED_DIGEST=sha256:abc123...
JUMI_INPUT_RESULT_NODE_LOCAL_PATH=/jumi-node-artifacts/cas/sha256/abc123...
JUMI_INPUT_RESULT_LOCAL_PATH=/work/inputs/result
```

B의 nan/runtime helper는 다음을 수행한다.

1. node-local artifact 존재 확인
2. sha256 digest 검증
3. `/work/inputs/result`로 copy
4. copied input digest 확인, optional
5. B command 실행

v0에서는 symlink/hardlink를 사용하지 않는다.

## 17. 컴포넌트 책임

### 17.1 nan / runtime helper

producer-side 책임:

- output artifact 확인
- digest 계산
- node-local artifact area로 copy promotion
- CAS overwrite/idempotency 정책 준수
- nonce 포함 tmp path 사용
- output manifest 작성
- promotion/manifest 실패 시 exit 1

consumer-side 책임:

- materialization mode 확인
- `LocalReuseMaterializer` 선택
- node-local CAS artifact digest 검증
- `/work/inputs`로 copy
- command 실행 전 input 준비 완료

### 17.2 JUMI

JUMI는 orchestration/control layer다.

책임:

- DAG node 실행 순서 관리
- A Pod/Job 생성 요청
- A output manifest 회수
- AH `RegisterArtifact` 호출
- B 실행 전 AH `ResolveBinding` 호출
- B Pod/Job 생성 시 placement/materialization 정보 전달

비책임:

- 대용량 artifact 직접 복사
- artifact backend별 전송 로직
- cache eviction
- peer transfer
- object storage upload/download 직접 처리

### 17.3 AH

AH는 artifact metadata와 location-aware materialization 판단을 담당한다.

책임:

- artifact logical identity 관리
- digest/size/producer attempt 기록
- `locations[]` 기록
- `node_local` location 이해
- B 실행 전 placement/materialization candidate 반환

### 17.4 spawner

spawner는 JUMI의 placement hint를 Kubernetes PodSpec으로 변환한다.

Sprint 3B happy path에서 필요한 것:

```text
RequiredNodeName → nodeSelector["kubernetes.io/hostname"]
```

## 18. Sprint 3B 성공 기준

Sprint 3B는 다음이 확인되면 성공으로 본다.

1. Sprint 3B 시작 전 manifest/RegisterArtifact seam preflight가 완료된다.
2. A Pod가 특정 worker node에서 실행된다.
3. A가 output artifact를 생성한다.
4. producer-side promotion이 수행된다.
5. promotion은 `copy into CAS path` 방식으로 수행된다.
6. `/out` 원본 output은 유지된다.
7. nonce 포함 tmp path가 사용된다.
8. artifact가 node-local artifact area의 CAS 경로에 저장된다.
9. CAS overwrite/idempotency 정책이 지켜진다.
10. digest가 계산된다.
11. output manifest에 `logicalUri`, `producerAttemptId`, `digest`, `node_local location`이 기록된다.
12. `logicalUri`에는 `attemptId`가 포함되지 않는다.
13. `logicalUri`는 직접 파싱하지 않고 opaque string으로 다룬다.
14. Location은 `nodeLocal` oneof payload 형태로 표현된다.
15. JUMI가 AH에 `RegisterArtifact`를 호출한다.
16. AH가 artifact의 `node_local` location을 기억한다.
17. B 실행 전 AH가 `local_reuse` materialization candidate를 반환한다.
18. Sprint 3B에서는 `materializationCandidates[0]`만 사용한다.
19. B Pod가 A와 같은 worker node에 배치된다.
20. B Pod는 node-local artifact area를 RO로 mount한다.
21. B의 nan/runtime helper가 `LocalReuseMaterializer`를 사용한다.
22. B `local_reuse` materialization은 `copy into /work/inputs` 방식으로 수행된다.
23. digest 검증이 성공한다.
24. `/work/inputs/<inputName>`에 input이 준비된다.
25. B command가 성공한다.
26. 전체 DAG run이 성공한다.

실패 기준:

- A command가 성공해도 promotion 실패 시 A node 실패
- manifest 작성 실패 시 A node 실패
- CAS digest mismatch 시 A node 실패
- B `local_reuse` digest mismatch 시 B node 실패
- B가 `requiredNodeName` 대상 node에 scheduling되지 못하면 Sprint 3B happy path 실패

## 19. Sprint 3B에서 하지 않는 것

이번 Sprint 3B에서는 아래를 하지 않는다.

- S3 / MinIO object storage 연동
- Dragonfly 통합
- UDP 고속 전송 구현
- peer_fetch 구현
- external_command backend
- HTTP upload source 구현
- node-local cache eviction
- retry / resume / range fetch
- multi-input concurrent fetch policy
- post-scheduling `ResolveBinding`
- `required_node` Pending/Unschedulable timeout 정교화
- production-grade Artifact Source Registry
- production-grade `hostPath` 정책
- `AddLocation` / `UpdateLocations` API 구현
- provenance query 구현
- cross-run same-digest lookup 구현
- full ordered candidates planner 구현
- `availableTransports` 기반 placement 정책 구현
- cross-cluster `logicalUri` 구현

## 20. 남는 설계 이슈

### 20.1 hostPath hardening

Sprint 3B에서는 `hostPath`를 검증용으로 사용하지만, production default로 승격하려면 별도 설계가 필요하다.

### 20.2 cleanup 정책

node-local artifact area에는 artifact가 계속 쌓일 수 있다.

Sprint 3B에서는 cleanup을 완성하지 않는다.

### 20.3 same-node 배치 실패

Sprint 3B happy path에서는 B가 A와 같은 worker node에 뜨는 것을 전제로 한다.

후속 fallback:

```text
local_reuse 실패
  ↓
remote_fetch 또는 peer_fetch fallback
```

Sprint 3B에서는 이 fallback을 구현하지 않는다.

### 20.4 post-scheduling ResolveBinding

Sprint 3B에서는 하지 않는다.

### 20.5 location 추가 API

Sprint 3B에서는 initial `locations[]`만 등록한다.

후속 단계에서는 다음 API가 필요할 수 있다.

```text
AddLocation(artifactId, location)
UpdateLocations(artifactId, locations[])
```

### 20.6 provenance lineage

Sprint 3B에서는 provenance slot만 둔다.

### 20.7 transport-aware placement

후속 단계에서는 transport capability에 따라 placement 강도가 달라져야 한다.

Sprint 3B에서는 `availableTransports = ["node_local"]`로 고정 가정한다.

### 20.8 full ordered candidates planner

Sprint 3B에서는 `materializationCandidates[0]`만 사용한다.

## 21. 추천 Sprint 이름

문서 제목:

```text
Sprint 3B = Pure K8s Node-local Handoff Happy Path
```

짧은 티켓 이름:

```text
Same-node Local Reuse Handoff
```

설계 목표 문장:

```text
Sprint 3B validates pure K8s same-node local_reuse handoff and producer-side node-local artifact promotion.
```

## 22. 최종 정리

Sprint 3B는 기존의 “producer publish/upload path”라는 표현보다 더 좁고 명확하게 정의한다.

기존 표현:

```text
producer publish/upload path
```

업데이트된 표현:

```text
pure K8s same-node local_reuse handoff
+ producer-side node-local artifact promotion
```

최종 happy path:

```text
A Pod on worker-2
  ↓
A가 artifact 생성
  ↓
producer-side copy promotion
  ↓
worker-2 node-local artifact area의 CAS path에 저장
  ↓
manifest에 logicalUri + producerAttemptId + digest + nodeLocal location 기록
  ↓
JUMI RegisterArtifact
  ↓
AH ResolveBinding
  ↓
materializationCandidates[0] = local_reuse
  ↓
B Pod도 worker-2에 배치
  ↓
B LocalReuseMaterializer
  ↓
CAS artifact digest 검증
  ↓
/work/inputs 로 copy
  ↓
B command 성공
```

한 줄 결론:

Sprint 3B는 외부 저장소 업로드가 아니라, pure K8s 환경에서 A가 만든 artifact를 같은 worker node의 node-local area로 copy promotion하고, B가 이를 `local_reuse`로 copy materialization하는 happy path를 검증하는 단계다.

단, 데이터 모델은 future Dragonfly, UDP, object_store 확장과 충돌하지 않도록 `Location` union, `ResolveBinding` candidates, `Materializer` interface, CAS identity/location 분리 원칙을 예약한다.
