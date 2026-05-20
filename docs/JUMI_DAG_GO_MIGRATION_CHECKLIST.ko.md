# JUMI dag-go Migration Checklist

> 작성일: 2026-05-20
> 목적: JUMI가 `github.com/HeaInSeo/dag-go` 정본으로 전환할 때 사용한 체크리스트와 후속 검증 기준을 고정한다.

## 기준

- 정본 저장소: `github.com/HeaInSeo/dag-go`
- 제거 대상 경로: `github.com/seoyhaein/dag-go`
- 원칙: JUMI는 항상 최신 `dag-go`를 우선 사용한다.

## 현재 상태

- JUMI는 `github.com/HeaInSeo/dag-go`를 사용한다.
- 제거 대상 경로 `github.com/seoyhaein/dag-go`는 새 업데이트 기준으로 더 이상 사용하지 않는다.
- 라이브러리 업데이트 시에는 항상 최신 `HeaInSeo/dag-go`를 우선 적용하고 전체 테스트/스모크를 다시 수행한다.

## 전환 선행 조건

1. `dag-go` 저장소의 `go.mod`가 `module github.com/HeaInSeo/dag-go`를 선언해야 한다.
2. 최신 커밋이 그 경로로 `go list -m` 가능한 상태여야 한다.
3. JUMI가 참조하는 최신 버전이 `HeaInSeo/dag-go` 경로로 published 되어야 한다.

## JUMI 전환 작업

1. `go.mod`의 `github.com/seoyhaein/dag-go`를 `github.com/HeaInSeo/dag-go`로 교체
2. import path를 `github.com/HeaInSeo/dag-go`로 교체
3. `go mod tidy`
4. `go test ./...`
5. `100.123.80.48`에서 build / smoke 재검증

## 검증 기준

- `go list -m all | rg dag-go` 결과가 `github.com/HeaInSeo/dag-go`만 보여야 한다.
- `go test ./...` 통과
- live smoke 통과
- JUMI ↔ AH ↔ nan runtime contract 회귀 없음

## 태그 판단 기준

아래가 모두 충족되어야 `dag-go` 전환 포함 태그 후보로 본다.

- `HeaInSeo/dag-go` 경로 전환 완료
- `spawner` 최신 반영 완료
- live smoke 통과
- known fail-open 이슈 없음
