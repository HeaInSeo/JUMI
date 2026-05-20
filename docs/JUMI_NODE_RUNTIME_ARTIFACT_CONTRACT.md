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

## 12. TODO

- TODO(runtime-contract): introduce an explicit node runtime base image containing the artifact helper.
- TODO(runtime-contract): replace the legacy `jumi-output-helper` path with `/usr/local/bin/nan` after the node runtime base image is introduced.
- TODO(runtime-contract): move the runtime helper and integration contract docs into a separate GitHub repo once the delivery contract is stable.
- TODO(runtime-contract): document contract-file injection only as a later-stage option for richer runtime configuration and input acquisition.
