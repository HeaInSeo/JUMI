# JUMI - AH - nan 연동 소스 리뷰

문서 상태: Draft v0.1  
작성일: 2026-05-17  
대상 프로젝트: `JUMI`, `artifact-handoff(AH)`, `node-artifact-runtime(nan)`

## 목적

현재 JUMI 코드에서 AH/nan 연동상 먼저 고쳐야 할 문제를 개발 순서대로 정리한다.

핵심 실행 체인은 아래와 같다.

```text
parent node Pod 성공
  ↓
nan이 output manifest 작성
  ↓
JUMI가 manifest 회수
  ↓
JUMI가 AH RegisterArtifact 호출
  ↓
child node 시작 전 JUMI가 AH ResolveHandoff 호출
  ↓
JUMI가 PlacementIntent / MaterializationPlan을 PodSpec 또는 minimal runtime context에 반영
  ↓
child node 실행
```

## 잘 잡힌 부분

- AH는 `ResolutionStatus`, `PlacementIntent`, `MaterializationPlan` 모델을 이미 갖고 있다.
- AH artifact identity에 `producerAttemptID`가 들어가 있다.
- AH RegisterArtifact는 idempotent 방향이 맞다.
- JUMI에는 ResolveBinding / RegisterArtifact / NotifyNodeTerminal hook 위치가 들어가기 시작했다.
- nan에는 output manifest writer의 기본 골격이 존재한다.

## 핵심 문제 요약

| 우선순위 | 이슈 | 현재 위험 | 권장 조치 |
|---|---|---|---|
| P0 | JUMI가 AH `ResolutionStatus`를 거의 해석하지 않음 | 실패/정책차단 상태에서도 child 실행 가능 | status별 fail/retry/block 정책 구현 |
| P0 | output metadata 회수 실패가 조용히 nil 처리됨 | manifest 없이 artifact 등록 가능 | strict metadata 모드 도입 |
| P0 | digest 없는 fallback URI 등록 가능 | 검증되지 않은 artifact 등록 | required output은 digest/uri 없으면 등록 금지 |
| P0 | directK8sHandle에서 output metadata 회수 미지원 | direct Job path에서 manifest 회수 불가 | direct handle도 Pod manifest 회수 지원 |
| P1 | RegisterArtifact에 producer NodeName 누락 | locality decision 약화 | Pod `Spec.NodeName`을 metadata/register에 포함 |
| P1 | PlacementIntent가 Pod placement에 반영되지 않음 | same-node/locality 정책이 실제로 동작하지 않음 | nodeAffinity 또는 post-scheduling resolve |
| P1 | MaterializationPlan이 env 주입만 되고 실제 materialization 없음 | child가 input을 실제로 못 읽을 수 있음 | v0는 contract만, v1은 init/nan acquisition |
| P1 | legacy `jumi-output-helper` path 사용 | node-artifact-runtime 분리와 충돌 | `/usr/local/bin/nan run`으로 전환 |
| P1 | manifest에 attemptID/schemaVersion 없음 | retry/동시 실행에서 manifest 식별 약함 | manifest v1 도입 |
| P1 | Pod 선택이 `pods.Items[0]` | 잘못된 Pod manifest 회수 가능 | succeeded main container 기준 선택 |
| P2 | termination-log 전체 manifest 의존 | truncation/parse 실패 가능 | summary/path 위주로 축소 |
| P2 | shell wrapper mode 유지 | quoting/path/security 리스크 | 제거 또는 deprecated |
| P2 | `node.succeeded`가 artifact 등록 전 발생 | 이벤트 순서 혼란 | RegisterArtifact 성공 후 succeeded 기록 |
| P2 | clean build 관점 local replace 리스크 | clone/CI 환경에서 깨질 수 있음 | versioned dependency 정리 |

## 우선 수정 순서

### Phase 1: Fail-closed safety

목표: 잘못된 artifact 등록과 child 실행을 막는다.

- AH `ResolutionStatus` 전체 처리
- output metadata unavailable strict error
- required output manifest 누락 시 node failed
- digest 없는 required artifact 등록 금지
- unknown status는 fail closed

### Phase 2: nan runtime shim 전환

목표: JUMI shell wrapper와 legacy helper를 `nan run` + minimal runtime context로 전환한다.

- `nan run / inspect / version`
- manifest schema v1
- attemptID 추가
- atomic write
- secure output path
- JUMI command injection을 `nan run -- <cmd>`로 변경
- full contract file injection은 보류하고 env/flag 기반 minimal context를 우선 사용

참고:
- JUMI 저장소 내부 helper source(`cmd/jumi-output-helper`, `pkg/runtimehelper`)는 제거됐다.
- 다만 `runtime-helper`, `wrapped-shell` 같은 obsolete compatibility 경로와 smoke shortcut fixture는 아직 남아 있다.
- 이들은 모두 새 구현의 기준 경로가 아니라, 제거 전 마지막 migration compatibility로만 간주한다.

### Phase 3: locality 반영

목표: AH location-aware decision이 실제 Pod 배치와 등록에 반영된다.

- `OutputMetadata.NodeName` 추가
- RegisterArtifact에 NodeName 전달
- `PlacementIntent`를 PodSpec에 반영
- required node conflict 처리

참고:
- 현재 JUMI는 `required_node`를 `kubernetes.io/hostname` nodeSelector로 materialize한다.
- 현재 JUMI는 `preferred_node`를 backend preferred placement로 전달하고, spawner K8s driver는 이를 `preferredDuringSchedulingIgnoredDuringExecution` nodeAffinity로 매핑한다.
- `preferred_node`는 여전히 soft hint다. Kubernetes scheduler가 이를 반드시 지킬 필요는 없고, locality miss는 계속 runtime variance로 다룬다.
- 현재 live smoke는 pre-scheduling planning-mode resolve만 사용한다.
- 이 경로에서는 AH가 `remote_fetch` 계획을 반환해도 `ah_fallback_total`을 올리지 않는 것이 정상이다.
- 즉 `jumi_input_remote_fetch_total` 증가와 `ah_fallback_total == 0`은 동시에 성립할 수 있다.
- `ah_fallback_total`은 target node가 구체화된 post-scheduling fallback execution 의미로 해석해야 한다.

### Phase 4: materialization baseline

목표: remote_fetch/local_reuse가 실제 input 준비로 이어진다.

- `MaterializationPlan`을 runtime context 또는 후속 contract 구조에 기록
- node-local reuse / peer fetch / nan runtime acquisition 설계
- init-container + emptyDir는 fallback 후보로만 검토
- digest verification
- `/in` read-only contract 검증

중요한 정의:

- `remote_fetch`는 특정 다운로드 기술 이름이 아니다.
- `remote_fetch`는 현재 실행 node에서 artifact를 사용할 수 있게 준비하는 materialization 동작이다.
- `MaterializationPlan.mode`는 "무엇을 해야 하는가"를 뜻한다.
- transport backend는 "어떤 수단으로 가져올 것인가"를 뜻한다.
- `jumi://...`는 logical artifact URI로 유지할 수 있고, fetchable URI와 동일할 필요는 없다.

transport backend 후보:

- `node_peer_fetch`
- `dragonfly`
- `external_command`
- `simple_http`
- `disabled` / `local_only`

원칙:

- AH/JUMI/nan은 특정 transport backend API에 직접 종속되지 않아야 한다.
- Dragonfly는 대용량 artifact 전송에 유리한 선택지일 수 있지만, 필수 구성요소는 아니다.
- pipeline spec은 `consumePolicy` 같은 분석 의미를 표현하고, runtime/materialization profile이 실제 transport strategy를 선택해야 한다.
- `simple_http`는 pipeline spec이 아니라 runtime/materialization profile 또는 VM test profile에서만 선택된다.

예:

```yaml
input:
  consumePolicy: SameNodeThenRemote
```

```yaml
materializationProfile:
  remoteFetch:
    strategy: simple_http
```

또는:

```yaml
materializationProfile:
  remoteFetch:
    strategy: external_command
    command: /opt/site/bin/fetch-artifact
```

유전체 분석 관점:

- FASTQ / BAM / CRAM / VCF는 수십 GB~수백 GB가 될 수 있다.
- 동일 artifact를 여러 자식 node가 반복 소비할 수 있다.
- 중앙 저장소 직접 다운로드만 반복하면 storage/네트워크 병목이 생긴다.
- digest 기반 검증은 필수다.
- node-local cache, retry / resume / partial download는 후속 확장 포인트다.

개발 경계:

- v0 목표
  - `remote_fetch` mode와 `uri` / `expectedDigest` 계약 유지
  - JUMI가 `MaterializationPlan`을 env 또는 node runtime context로 전달
  - `nan` 또는 runtime helper가 `simple_http` backend로 작은 파일 happy path를 검증
  - transport backend abstraction을 문서로 고정
- v0 비목표
  - Dragonfly 완전 통합
  - node-local CAS cache 완성
  - peer fetch 완성
  - `direct_object_store` 일반화
  - 기관별 전송 도구 완전 통합
  - post-scheduling resolve 완성
- v1 이후 후보
  - node-local CAS cache
  - Dragonfly backend
  - peer fetch backend
  - external command backend
  - transfer metrics
  - fetch retry / resume
  - large-file stress test
  - `targetNodeName=actualNode` 기반 post-scheduling resolve

VM happy path 계획:

1. A node가 작은 artifact를 생성한다
2. `nan`이 output manifest를 만든다
3. v0 VM happy path용 runtime/materialization profile adapter가 A output을 `simple_http` artifact source로 노출한다
4. JUMI가 AH에 `RegisterArtifact` 한다
5. AH에는 fetch 가능한 `http://...` URI가 등록된다
6. AH가 B input `ResolveBinding`을 수행한다
7. AH가 `remote_fetch` `MaterializationPlan`을 반환하고, `MaterializationPlan.URI`는 등록된 `http://...` URI를 pass-through 한다
8. JUMI가 B env/runtime context에 `uri`, `expectedDigest`, `materialization mode`를 전달한다
9. B node가 `simple_http` backend로 artifact를 fetch한다
10. digest를 검증한다
11. `/work/inputs/<inputName>`에 atomic하게 배치한다
12. B가 input을 읽고 성공한다
13. 이후 `10MB -> 1GB -> 5GB+`로 확장한다

이 검증의 목적은 최종 전송 기술을 고정하는 것이 아니라, `remote_fetch` 계약이 실제로 artifact를 materialize할 수 있는지 확인하는 것이다.

## 개발 가드레일

- AH proto는 AH repo가 정본이다.
- JUMI는 DAG와 binding의 정본이다.
- AH는 DAG를 추론하지 않는다.
- nan은 AH에 직접 RegisterArtifact하지 않는다.
- nan은 Kubernetes API를 호출하지 않는다.
- required output은 digest 없이 등록하지 않는다.
- unknown status는 fail closed한다.
- manifest는 attemptID를 포함한다.
- artifact registration 성공 전 node succeeded로 확정하지 않는다.

## 최종 판단

현재 JUMI의 가장 큰 리스크는 기능이 부족하다는 것보다, 실패해야 할 상황에서 계속 진행할 수 있다는 점이다.

따라서 우선순위는 아래가 맞다.

```text
1. JUMI fail-closed safety
2. nan runtime shim contract 정리
3. manifest strict validation
4. NodeName/PlacementIntent 반영
5. materialization 구현
```
