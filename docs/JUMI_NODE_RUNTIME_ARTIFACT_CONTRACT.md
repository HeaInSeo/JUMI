# JUMI Node Runtime Artifact Contract

> 작성일: 2026-05-16
> 목적: `jumi-output-helper`와 `ko` 전환의 경계를 명확히 해서, helper가 JUMI 서비스 이미지 소속처럼 오해되는 것을 막는다.

## 1. 한 줄 정의

JUMI는 서비스 이미지와 DAG node runtime 이미지를 구분한다.

- JUMI 서비스 이미지는 `jumi` 프로세스를 실행한다.
- DAG node runtime 이미지는 실제 분석 도구와 runtime-side artifact helper를 포함한다.

즉 artifact helper는 JUMI 서버 프로세스의 일부가 아니라, node runtime container 안에서 실행되는 runtime-side executable이다.
현재 방향에서 이 executable은 "pipeline 의미를 해석하는 주체"가 아니라
"컨테이너 내부 실행 결과를 관찰하고 observed manifest를 생성하는 주체"다.

## 2. 두 종류의 이미지

### 2.1 JUMI Service Image

이 이미지는 JUMI service process를 실행한다.

포함 대상:

- `jumi`

역할:

- executable run spec 수신
- DAG execution coordination
- Kubernetes / spawner 경로 호출
- node completion / status observation
- AH seam integration

이 이미지는 execution coordinator다.
일반 pipeline tool image가 아니다.

### 2.2 DAG Node Runtime Image

이 이미지는 각 DAG node가 실제로 사용하는 runtime image다.

예:

- BWA node image
- GATK node image
- Samtools node image
- Python / script node image

이 이미지의 역할:

- 사용자 command 실행
- declared output 생성
- runtime-side helper를 통한 artifact metadata export

JUMI는 각 DAG node/tool 단위로 자원 할당과 스케줄링을 보게 되므로,
장기적으로는 각 tool image가 하나의 주 도구를 중심으로 유지되는 것이 맞다.

## 3. Helper 의 위치

현재 legacy 호환 이름:

- `jumi-output-helper`

프로젝트 이름:

- `node-artifact-runtime`

의도된 runtime binary 이름:

- `nan`

중요:

- `jumi-output-helper`는 최종적으로 삭제할 legacy compatibility 이름이다.
- JUMI 저장소 안의 helper 구현도 최종적으로 삭제 대상이다.
- 남겨야 하는 것은 helper 기능을 제공하는 별도 runtime artifact이지, JUMI 저장소 안의 helper copy가 아니다.

이 helper는 다음 성격을 가진다.

- JUMI service-side binary가 아니다.
- 일반 scheduler binary가 아니다.
- DAG node runtime container 안에서 실행되는 runtime-side executable이다.

즉 helper는 "JUMI server image에 함께 들어 있는 부속 바이너리"가 아니라,
"node runtime artifact contract를 수행하는 runtime-side executable"로 봐야 한다.

## 4. 왜 Runtime Container 안에서 실행돼야 하는가

artifact metadata는 node runtime container 안에서 생성된 실제 output을 기준으로 계산돼야 한다.

helper는 node runtime container 안에서 아래 일을 수행한다.

- 사용자 command wrapper 역할
- command 종료 code 보존
- `/out` 산출물 관찰
- digest 계산
- sizeBytes 계산
- observed artifact manifest 생성
- termination log 또는 후속 채널로 metadata 전달

이 observed manifest를 JUMI가 읽고,
JUMI가 자신이 이미 알고 있는 declared output / attempt / artifact policy와 합쳐서
AH RegisterArtifact를 수행한다.

즉 helper는 artifact registration의 최종 의미를 소유하지 않는다.
그 역할은 JUMI에 남겨둔다.

## 5. Runtime Base Image 모델

장기적으로는 helper가 들어 있는 공통 runtime base image를 두고,
실제 tool image들이 그 base image를 상속받는 구조가 맞다.

이 runtime base image는 JUMI 전용 artifact가 아니라,
다른 프로젝트에서도 재사용 가능한 runtime-side artifact contract 자산으로 보는 것이 맞다.

즉:

- helper는 JUMI service image에 속하지 않는다
- helper는 AH service image에 속하지 않는다
- helper는 node runtime base image에 속한다
- tool image를 만드는 다른 프로젝트도 이 base image를 상속할 수 있다
- base image packaging 자체는 NodeKit 또는 NodeVault 같은 별도 계층이 담당할 수 있다

예:

```text
node-artifact-runtime-base
  - /usr/local/bin/nan
```

```text
bwa image
  FROM node-artifact-runtime-base
  installs bwa
```

```text
gatk image
  FROM node-artifact-runtime-base
  installs gatk
```

이 모델에서는:

- helper는 node runtime base image의 일부다.
- tool image는 base image를 상속한다.
- JUMI service image는 별도로 유지된다.

## 6. Command Wrapping 모델

runtime helper 경로는 개념적으로 아래와 같다.

```text
/usr/local/bin/nan run -- <user command>
```

현재 기준 경로는 `nan`이며, legacy 이름은 호환 alias로만 남긴다.

```text
/usr/local/bin/jumi-output-helper run -- <user command>
```

호환성 규칙:

- 오래된 runtime image는 `/usr/local/bin/jumi-output-helper` alias를 여전히 가질 수 있다
- 하지만 JUMI service image는 더 이상 이 legacy alias를 기본 제공 대상으로 보지 않는다
- migration이 끝날 때까지 canonical path는 `/usr/local/bin/nan`이다

## 7. Current v0 Behavior

현재 helper의 최소 동작은 다음이다.

- user command 실행
- exit code 보존
- `/out` 산출물 관찰
- observed artifact manifest 작성
- JUMI가 manifest를 읽고 AH에 register

즉 source of truth는 helper가 아니라,
helper가 export한 observed manifest와 JUMI가 가진 declared contract를 합친 결과다.

다시 말해:

- `nan`은 observed manifest producer다.
- JUMI는 semantic interpreter이자 AH coordinator다.

## 8. Future Direction

향후 강화 방향:

- optional direct communication with JUMI
- explicit `attemptId` / `runId` / `nodeId` contract
- separate helper project / separate GitHub repo
- independently versioned helper artifact and node runtime base image
- optional contract file injection when runtime configuration becomes more complex

현재 단계에서 `contract file`은 필수 전제가 아니다.
`nan`이 더 많은 책임을 가져야 할 때의 확장점으로 남겨둔다.

초기 단계의 minimal runtime context 예:

- `JUMI_RUN_ID`
- `JUMI_NODE_ID`
- `JUMI_ATTEMPT_ID`
- `JUMI_OUTPUT_ROOT`
- `JUMI_OUTPUT_MANIFEST_PATH`

이 문서 기준으로 보면, helper는 장기적으로 JUMI service repository와 느슨하게 결합된 별도 artifact가 되는 것이 맞다.

## 9. ko Migration Boundary

`ko` migration은 JUMI service image를 대상으로 해야 한다.

JUMI와 같은 data-plane service app은 image build 경로를 장기적으로 `ko`로 통일하는 방향으로 간다.

즉:

- `ko` migration target: JUMI service image
- helper 소속: node runtime base image 또는 separate helper artifact

잘못된 방향:

- `ko`가 helper까지 JUMI service image 안에 계속 우겨 넣어야 한다고 보는 것

맞는 방향:

- `ko`는 `cmd/jumi` service image migration을 담당
- helper delivery는 node runtime artifact contract에서 별도로 다룸

## 10. Current Compatibility Note

현재 저장소에는 과거 smoke / dev shortcut 때문에
JUMI image를 node runtime image로도 사용한 흔적이 있다.

이건 production contract가 아니라 test convenience다.

즉 현재 JUMI/AH 통합 테스트가 tool image 또는 JUMI image shortcut을 쓰더라도,
그것이 helper의 최종 소속을 의미하지는 않는다.

실제 intended contract는 다음이다.

- JUMI service image는 service image다.
- DAG node runtime image는 tool image다.
- artifact helper는 node runtime image에 포함된다.

테스트에서는 아래 같은 shortcut이 허용될 수 있다.

- helper binary가 이미 들어 있는 JUMI image를 producer runtime image로 재사용
- helper binary가 이미 들어 있는 test tool image를 smoke fixture에 사용

하지만 production / long-term contract에서는 아래가 맞다.

- helper는 node runtime base image에 포함
- BWA / GATK / Samtools / Python tool image는 그 base image를 상속
- JUMI와 AH는 helper delivery 자체보다 helper contract 소비자 역할에 집중

## 11. Declared Contract vs Observed Manifest

현재 방향에서는 아래 둘을 명확히 구분한다.

### 11.1 Declared Contract

이건 JUMI가 이미 알고 있는 실행 의도다.

- 어떤 output이 선언되었는가
- 어떤 attempt가 실행 중인가
- 어떤 artifact policy가 적용되는가
- 어떤 binding을 AH에 등록/조회해야 하는가

### 11.2 Observed Manifest

이건 `nan`이 컨테이너 안에서 실제로 관찰한 결과다.

- 어떤 파일이 만들어졌는가
- 실제 digest/size는 무엇인가
- 어떤 attempt 문맥에서 관찰된 것인가

JUMI는 declared contract와 observed manifest를 합쳐서 AH에 등록한다.
따라서 `nan`이 pipeline semantics 전체를 직접 알 필요는 없다.

## 12. Remote Fetch 와 Materialization

현재 문맥에서 `remote_fetch`는 특정 전송 기술 이름이 아니다.

정의:

- `remote_fetch`는 `MaterializationPlan`에 포함된 `uri`와 `expectedDigest`를 기준으로, 현재 실행 노드에서 자식 작업이 사용할 수 있도록 부모 artifact를 준비하는 추상적인 materialization 동작이다.
- 즉 `remote_fetch`는 "무엇을 해야 하는가"를 뜻하고, 실제로 "어떤 수단으로 가져올 것인가"는 별도 transport backend가 결정한다.

반드시 구분해야 하는 두 층:

- `MaterializationPlan.mode`
  - 예: `none`, `local_reuse`, `remote_fetch`
  - 의미: 실행 시점에 어떤 종류의 artifact 준비가 필요한가
- `TransportStrategy` 또는 `FetchBackend`
  - 예: `direct_object_store`, `node_peer_fetch`, `dragonfly`, `external_command`, `disabled`
  - 의미: 실제 전송/준비를 어떤 수단으로 수행할 것인가

예시:

```text
mode: remote_fetch
uri: s3://bucket/path/to/output.bam
expectedDigest: sha256:...
transport strategy: direct_object_store
```

또는:

```text
mode: remote_fetch
uri: peer://worker-2/runs/run-001/outputs/aligned-bam
expectedDigest: sha256:...
transport strategy: node_peer_fetch
```

즉 `remote_fetch = 다운로드`로 설명하면 부족하다.
정확한 의미는 "현재 실행 노드에서 artifact를 사용할 수 있는 상태로 materialize하는 것"이다.

### 12.1 Logical URI 와 Fetchable URI

현재 문맥에서는 artifact URI를 두 층으로 구분하는 것이 안전하다.

- `logicalUri`
  - 예: `jumi://run/{runId}/node/{nodeId}/attempt/{attemptId}/output/{name}`
  - 의미: artifact의 내부 식별자 또는 논리적 URI
  - 반드시 실제 fetch 가능한 URL일 필요는 없다
- `fetchableUri`
  - 예: `http://artifact-source.local/artifacts/{digest}`
  - 의미: 특정 실행 환경에서 실제 바이트를 가져올 수 있는 transport URI
  - 환경별 backend와 source policy에 따라 달라질 수 있다

예:

```yaml
logicalUri: jumi://run/{runId}/node/{nodeId}/attempt/{attemptId}/output/{name}
fetchableUri: http://artifact-source.local/artifacts/{digest}
digest: sha256:...
```

현재 JUMI smoke와 `nan` manifest 경로에서는 `jumi://...`를 계속 logical URI로 사용할 수 있다.
장기적으로 fetchable URI의 source of truth는 AH 또는 그 뒤의 materialization source layer가 소유해야 한다.

## 13. Pipeline Spec 과 Runtime Materialization Profile 분리

pipeline spec에는 특정 전송 기술을 박지 않는다.

나쁜 예:

```yaml
input:
  fetch: dragonfly
```

좋은 예:

```yaml
input:
  consumePolicy: SameNodeThenRemote
```

실제 전송 방식은 runtime 또는 materialization profile에서 선택한다.

예:

```yaml
materializationProfile:
  remoteFetch:
    strategy: direct_object_store
```

또는:

```yaml
materializationProfile:
  remoteFetch:
    strategy: dragonfly
```

또는:

```yaml
materializationProfile:
  remoteFetch:
    strategy: external_command
    command: /opt/site/bin/fetch-artifact
```

원칙:

- pipeline spec은 분석 논리와 input 소비 정책을 표현한다.
- runtime/materialization profile은 실행 환경별 전송 구현을 선택한다.
- 같은 pipeline spec이 여러 기관/클러스터에서 서로 다른 transport backend로 실행될 수 있어야 한다.
- `simple_http` 같은 검증용 backend도 pipeline spec이 아니라 runtime/materialization profile 또는 VM test profile에서만 선택되어야 한다.

## 14. Runtime-side Transport Backend 후보

`nan` 또는 그 후속 runtime helper는 transport backend를 교체 가능한 계층으로 다뤄야 한다.

### 14.1 `direct_object_store`

- S3 / MinIO / HTTP / object storage에서 직접 다운로드
- v0 happy path 검증에 가장 적합
- 구현이 단순하다
- 대용량/동시성 상황에서는 object storage 병목이 생길 수 있다

### 14.2 `node_peer_fetch`

- artifact가 있는 worker node 또는 node-local cache에서 직접 가져온다
- AH의 location-aware 설계와 잘 맞는다
- 별도의 node-level artifact endpoint 또는 agent가 필요할 수 있다

### 14.3 `dragonfly`

- P2P distribution layer를 사용하는 backend 후보다
- 대용량 artifact, 반복 다운로드, 모델/OCI artifact 배포에 유리할 수 있다
- 하지만 모든 기관이나 연구소가 Dragonfly를 사용할 수 있는 것은 아니다
- 따라서 Dragonfly는 선택 가능한 backend일 뿐, AH/JUMI/nan의 필수 의존성이 아니다

정리 문장:

> Dragonfly는 P2P 전송이 가능한 선택적 backend이며, AH/JUMI/nan이 필수로 의존해야 하는 구성요소는 아니다.

### 14.4 `external_command`

- 기관/기업이 이미 보유한 고속 전송 도구나 사내 CLI를 호출한다
- 예: `/opt/site/bin/fetch-artifact`
- `uri`, `expectedDigest`, `outputPath` 같은 값을 인자로 넘기는 방식으로 설계할 수 있다

### 14.5 `disabled` 또는 `local_only`

- remote fetch를 허용하지 않는 환경
- `SameNodeOnly` 정책 또는 `required_node` 기반 실행만 허용
- remote fetch가 필요하면 실패 처리한다

### 14.6 `simple_http`

- v0 VM happy path 검증용 backend
- 작은 파일을 대상으로 단순 HTTP GET만 가정한다
- 최종 전송 구조를 고정하기 위한 것이 아니라, `remote_fetch` 계약이 실제 fetch/materialize로 이어지는지 빨리 검증하기 위한 backend다
- pipeline spec에는 드러나지 않고 runtime/materialization profile 또는 VM test profile에서만 선택된다

## 15. 유전체 분석 관점에서의 Remote Fetch

유전체 분석에서는 `remote_fetch`를 단순 다운로드로 보면 안 된다.

이유:

- FASTQ / BAM / CRAM / VCF는 수십 GB~수백 GB가 될 수 있다
- 동일 artifact를 여러 자식 node가 반복 소비할 수 있다
- 중앙 object storage에서 매번 다시 가져오면 네트워크와 storage가 병목이 될 수 있다
- digest 기반 검증은 필수다
- node-local cache는 v1 이후 중요한 확장 포인트다
- retry / resume / partial download / range access도 향후 중요한 확장 포인트다

현재 단계의 목표는 대용량 transport를 완성하는 것이 아니라, 작은 happy path를 direct backend로 먼저 검증하면서 transport abstraction을 설계 문서로 고정하는 것이다.

## 16. v0 / v1 경계

### 16.1 v0 목표

- `remote_fetch` mode와 `uri` / `expectedDigest` 계약 유지
- JUMI가 `MaterializationPlan`을 env 또는 후속 node contract 구조로 전달
- `nan` 또는 runtime helper가 `simple_http` backend로 실제 fetch 수행 가능
- digest 검증
- 작은 파일 기준 VM happy path 검증
- pipeline spec이 특정 transport backend에 종속되지 않음
- transport backend 교체 가능성을 문서로 고정

### 16.2 v0 비목표

- Dragonfly 완전 통합
- node-local CAS cache 완성
- peer fetch 완성
- `direct_object_store` 일반화
- 고급 retry / resume / range fetch
- 기관별 고속 전송 도구 완전 통합
- post-scheduling resolve 완성

### 16.3 v1 이후 후보

- node-local CAS cache
- Dragonfly backend
- peer fetch backend
- external command backend
- transfer metrics
- fetch retry / resume
- large-file stress test
- `targetNodeName=actualNode` 기반 post-scheduling resolve
- scheduling 결과 기반 materialization 재판단

## 17. VM Happy Path 검증 계획

초기 VM happy path는 Dragonfly가 아니라 `simple_http` backend를 기준으로 잡는다.

중요:

- 이것은 최종 전송 구조가 아니라, remote_fetch materialization smoke를 빠르게 닫기 위한 검증용 backend다.
- `simple_http` 선택은 pipeline spec이 아니라 runtime/materialization profile 또는 VM test profile에서 이뤄진다.
- 이 profile은 JUMI core의 일반 책임이 아니라, v0 happy path 검증을 위한 환경별 adapter 성격으로 본다.

예상 흐름:

1. A node가 작은 artifact를 생성한다
2. `nan` 또는 runtime helper가 output manifest를 생성한다
3. v0 VM happy path용 runtime/materialization profile adapter가 A output을 `simple_http` artifact source 아래에 노출한다
4. JUMI가 AH에 `RegisterArtifact` 한다
5. 이때 AH에 들어가는 fetchable URI는 `http://...` 형태다
6. AH가 B input에 대해 `ResolveBinding` 한다
7. AH는 등록된 `http://...` URI를 `MaterializationPlan.URI`로 pass-through 한다
8. JUMI가 B node env 또는 contract에 `uri`, `expectedDigest`, `materialization mode`를 전달한다
9. B node가 `simple_http` backend로 artifact를 fetch한다
10. digest를 검증한다
11. `/work/inputs/<inputName>`에 atomic하게 준비한다
12. B가 input을 읽고 성공한다
13. 이후 파일 크기를 `10MB -> 1GB -> 5GB+` 순서로 확장한다

핵심 문장:

> VM happy path의 목적은 최종 전송 기술을 결정하는 것이 아니라, `remote_fetch` 계약이 실제 실행 흐름에서 artifact를 materialize할 수 있는지 검증하는 것이다.

## 18. Known Gaps

- `directK8s` fallback path는 `PreferredNodes` / `nodeAffinity` semantics를 잃을 수 있다
- `required_node` pending / unschedulable timeout 정책은 아직 더 명확해져야 한다
- `ResolveBinding(targetNodeName=actualNode)` 기반 post-scheduling re-resolve는 아직 구현되지 않았다

## 19. TODO

- TODO(runtime-contract): introduce an explicit node runtime base image containing the artifact helper.
- TODO(runtime-contract): replace the legacy `jumi-output-helper` path with `/usr/local/bin/nan` after the node runtime base image is introduced.
- TODO(runtime-contract): move the runtime helper and integration contract docs into a separate GitHub repo once the delivery contract is stable.
- TODO(runtime-contract): document contract-file injection only as a later-stage option for richer runtime configuration and input acquisition.
