# JUMI Node Runtime Base Image Plan

> 작성일: 2026-05-16
> 목적: `node-artifact-runtime`의 장기 소속을 node runtime base image로 고정하고, JUMI/AH와의 책임을 분리한다.

## 1. 한 줄 정의

`node-artifact-runtime`은 JUMI service image가 아니라 node runtime base image에 포함되어야 한다.

## 2. 왜 base image가 필요한가

현재 helper는 DAG node runtime container 안에서 다음 일을 수행한다.

- user command wrapper
- output inspection
- digest / sizeBytes 계산
- artifact manifest 생성
- termination log export

이 역할은 JUMI service process 바깥,
실제 tool runtime environment 안에서 필요하다.

그래서 helper는 service image보다 runtime base image에 놓는 것이 맞다.

## 3. intended image model

예시:

```text
node-artifact-runtime-base
  - /usr/local/bin/node-artifact-runtime
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

```text
samtools image
  FROM node-artifact-runtime-base
  installs samtools
```

## 4. JUMI / AH 책임

JUMI 책임:

- helper path/env contract 전달
- manifest readback
- artifact registration with AH

AH 책임:

- registered artifact inventory 유지
- child handoff resolution
- lifecycle / finalize / GC seam

둘 다 helper binary delivery 자체를 소유하지 않는다.

즉 helper는 JUMI/AH의 이미지 내부 자산이 아니라,
runtime contract의 일부다.

base image packaging과 publish는 장기적으로 NodeKit 또는 NodeVault 계열 repo가 맡는 것이 맞다.

## 5. 테스트와 production 구분

현재 smoke나 dev 경로에서는 아래 같은 shortcut이 있을 수 있다.

- JUMI image를 producer runtime image로 사용
- helper가 이미 들어 있는 test image를 tool image 대신 사용

하지만 production contract는 다르다.

- production tool image는 runtime base image를 상속
- helper는 runtime base image에 포함
- JUMI/AH는 helper contract 소비자

## 6. helper naming

현재 compatibility name:

- `jumi-output-helper`

의도된 conceptual name:

- `node-artifact-runtime`

migration 동안에는 legacy path를 유지할 수 있지만,
base image가 들어오면 새 이름으로 옮기는 것이 맞다.

## 7. 단계별 작업

1. helper contract 문서 고정
2. helper path override / compatibility 유지
3. runtime base image 정의
4. representative tool image 하나를 base image 상속 방식으로 전환
5. smoke fixture에서 JUMI image shortcut 제거
6. legacy helper path 제거
7. helper source를 별도 GitHub repo로 이동
8. NodeKit / NodeVault에서 runtime base image definition과 publish pipeline을 관리

## 8. 대표 검증 질문

runtime base image 도입 후 확인해야 할 질문:

- tool image가 helper를 안정적으로 포함하는가
- helper wrapper가 user command exit code를 정확히 보존하는가
- manifest contract가 JUMI/AH와 계속 호환되는가
- tool image 종류가 달라도 helper contract가 동일하게 유지되는가

## 9. follow-up

- NodeKit / NodeVault base image publish pipeline과 contract alignment 확인
- helper versioning strategy 결정
- tool image release process와의 결합도 정리
