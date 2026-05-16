# JUMI ko Service Image Migration Plan

> 작성일: 2026-05-16
> 목적: `ko` 전환 범위를 JUMI service image로 한정하고, node runtime artifact helper 문제와 섞이지 않게 한다.

## 0. Platform Direction

JUMI와 같은 Kubernetes data-plane service app은 image build 경로를 `ko`로 통일하는 방향으로 간다.

즉 장기 원칙은 아래와 같다.

- data-plane service app image build: `ko`
- Harbor publish: `ko` output 기준
- K8s deployment image update: `ko`로 만든 service image 기준

이 원칙은 service/process image에 적용된다.

적용 예:

- JUMI service image
- 향후 같은 성격의 다른 data-plane app service image

비적용 예:

- runtime-side helper binary delivery
- tool image packaging
- node runtime base image packaging

## 1. 범위

이 문서의 범위는 JUMI service image다.

대상:

- `cmd/jumi`
- JUMI service deployment
- Harbor push
- K8s deployment image update

비대상:

- `jumi-output-helper` / `node-artifact-runtime`
- node runtime base image
- tool image delivery
- helper delivery contract
- separate helper/base-image repository delivery

## 2. 한 줄 원칙

`ko` migration은 JUMI service image만 다룬다.

동시에, data-plane service app은 `ko`로 통일하는 것이 기본 방향이다.

즉 `ko`는 아래만 만족하면 된다.

- `cmd/jumi`를 clean checkout 기준으로 빌드
- Harbor에 publish
- K8s deployment에서 그 이미지를 사용

helper delivery 문제를 `ko`로 우회해서 해결하려고 하면 안 된다.

## 3. 선행 조건

`ko` 전환 전에 만족해야 하는 조건:

1. published dependency만으로 clean checkout build가 가능해야 한다
2. local `replace ../spawner` 같은 sibling checkout 전제가 없어야 한다
3. `go test ./...`가 published dependency 기준으로 통과해야 한다

현재 JUMI는 이 방향으로 정리 중이며,
서비스 이미지 전환은 이 선행 조건 위에서만 진행해야 한다.

## 4. service image가 포함해야 하는 것

JUMI service image는 아래만 포함하는 방향이 맞다.

- `/usr/local/bin/jumi`

현재 compatibility 때문에 helper를 같이 복사하는 경로가 일부 남아 있더라도,
그것은 migration 중 임시 상태일 뿐 장기 계약이 아니다.

## 5. 서비스 이미지 전환 단계

1. `ko` build target을 `./cmd/jumi`로 고정
2. Harbor repository 경로를 명시
3. generated image ref를 deployment update 경로에 연결
4. live smoke는 JUMI service deployment 교체 관점에서만 검증

즉 `ko`의 성공 조건은:

- JUMI service pod가 새 이미지로 정상 기동
- northbound RPC와 AH/K8s 연동이 깨지지 않음

이다.

## 6. 하면 안 되는 것

- helper를 계속 service image 안에 넣는 것을 `ko` 전환의 핵심 문제로 보는 것
- tool image contract와 service image contract를 한 번에 풀려는 것
- node runtime base image 설계가 끝나기 전에 helper delivery까지 `ko`에서 책임지려는 것

## 7. live smoke 해석

live smoke에서 producer runtime image가 여전히 JUMI image shortcut을 쓸 수 있다.

하지만 그건 다음을 의미하지 않는다.

- JUMI service image가 production에서도 helper를 소유해야 한다

그건 단지 현재 smoke fixture의 편의 경로일 뿐이다.

## 8. 다음 단계

- explicit `ko` build/apply script 추가
- Harbor push 경로 표준화
- `jumi-ah-dev` deployment image 교체 자동화
- node runtime base image 도입 후 helper compatibility copy 제거
