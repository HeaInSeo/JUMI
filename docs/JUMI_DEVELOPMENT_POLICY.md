# JUMI Development Policy

작성일: 2026-05-31  
적용 범위: JUMI, artifact-handoff, node-artifact-runtime

---

## 1. 핵심 원칙

### Pure하게 간다

외부 의존성은 꼭 필요할 때만 추가한다.

- 표준 라이브러리로 해결 가능하면 외부 패키지를 쓰지 않는다.
- subcommand dispatch, flag 파싱 등은 `os.Args`, `flag` 패키지로 처리한다.
- framework(cobra, viper 등)는 표준 라이브러리가 명백히 부족할 때만, 그리고 사전 결정 후 도입한다.

### 우회하지 않는다

문제가 생겼을 때 증상만 없애는 방식으로 해결하지 않는다.

- lint/vet 오류는 `//nolint` 또는 `_ =` 로 억제하기 전에 근본 원인을 먼저 확인한다.
- `--no-verify`, `--force`, `set +e` 등 안전장치를 끄는 명령은 이유를 명시하고 최소 범위에서만 쓴다.
- 임시 경로(`/tmp/...`)에 레포를 두지 않는다. 정규 Go 프로젝트 경로를 쓴다.

### 결정이 필요한 것은 먼저 묻는다

agent가 독자적으로 판단하지 말아야 할 항목:

- 외부 의존성 추가
- 브랜치 생성 / force push
- 인프라 경로 변경 (레지스트리, 엔드포인트, 베이스 이미지 등)
- 기존 인터페이스/계약 변경
- 삭제 작업 (파일, 디렉토리, 브랜치)

결정 전에 선택지를 제시하고 사용자의 답을 기다린다.

---

## 2. 브랜치 정책

- **단일 브랜치**: `main` 하나만 운용한다.
- feature 브랜치, PR 브랜치를 만들지 않는다.
- GitHub이 정본이다. 로컬 커밋은 작업 완료 후 바로 push한다.

---

## 3. 빌드 정책

### Go 바이너리

- `CGO_ENABLED=0` 유지. 동적 링크가 필요한 의존성은 사전 결정 후 도입한다.
- SQLite가 필요해지면 `modernc.org/sqlite` (pure Go)를 쓴다. `mattn/go-sqlite3`(CGo)는 쓰지 않는다.

### 컨테이너 이미지

- Ko를 사용한다. `podman build`, `docker build`는 Ko가 해결 못 하는 경우에만 쓴다.
- 베이스 이미지: `gcr.io/distroless/static-debian12:nonroot` (CGO_ENABLED=0 기준).
  - libc가 필요한 경우: `gcr.io/distroless/base-debian12:nonroot`로 변경, 단 사전 결정 필요.
- ENTRYPOINT/CMD는 exec form만 사용한다.
- 디버깅은 `kubectl debug` + 별도 debug image로 분리한다. 서비스 이미지에 shell 넣지 않는다.

### 바이너리 구조

- 여러 실행 파일이 필요하면 `cmd/<name>/` 디렉토리를 추가한다.
- operator 툴은 별도 바이너리를 만들기 전에 기존 바이너리의 subcommand로 통합할 수 있는지 먼저 검토한다.
- subcommand dispatch는 `os.Args` + `flag.NewFlagSet`으로 구현한다.

---

## 4. 코드 정책

### 의존성

- `go.mod`에 없는 패키지를 임의로 추가하지 않는다. 추가 전 사용자에게 확인한다.
- `k8s.io/**`, `sigs.k8s.io/**`는 node-artifact-runtime(nan)에 금지한다. nan은 컨테이너 안에서 실행되는 standalone 런타임이다.

### 오류 처리

- `errcheck` 기준을 따른다. 반환 오류를 무시할 때는 `_ =`와 이유 주석을 함께 쓴다.
- defer 안에서 오류가 나는 Close/Remove는 `defer func() { _ = f.Close() }()` 패턴을 쓴다.

### 파일 권한

- 컨테이너 내부에서 생성되는 디렉토리: `0o750`
- 컨테이너 내부에서 생성되는 파일: `0o640` 또는 `0o600`
- 테스트 픽스처 파일: `0o600`

### 주석

- 코드가 무엇을 하는지 설명하는 주석은 쓰지 않는다.
- 왜 그렇게 해야 하는지 (숨겨진 제약, 특정 버그 우회, 보안 의도)가 비자명할 때만 한 줄로 쓴다.

---

## 5. 린트 / 보안 정책

세 레포 모두 동일한 golangci-lint 설정을 유지한다.

활성 linter: `errcheck`, `gosec`, `govet`, `staticcheck`, `unused`, `revive`, `depguard`, `ineffassign`, `misspell`

`gosec` 억제(`//nolint:gosec`)는 다음 경우에만 허용한다:
- G204: nan이 의도적으로 operator 제어 명령을 실행하는 경우
- G304: operator가 제어하는 경로(플래그/환경변수)에서 파일을 여는 경우

억제 시 반드시 이유를 주석으로 명시한다.

---

## 6. 테스트 정책

- 새 public 함수/타입에는 테스트를 함께 작성한다.
- 외부 서비스(k8s, AH gRPC 등)를 직접 호출하는 테스트는 `//go:build integration` 태그로 분리한다.
- 단위 테스트에서 실제 k8s/네트워크를 사용하지 않는다.

---

## 7. 세션 시작 정책

모든 agent/세션은 코드 작업 전에 아래를 먼저 실행한다.

```bash
# 세 레포 동기화 확인
git -C /opt/go/src/github.com/HeaInSeo/JUMI fetch origin
git -C /opt/go/src/github.com/HeaInSeo/artifact-handoff fetch origin
git -C /opt/go/src/github.com/HeaInSeo/node-artifact-runtime fetch origin

git -C /opt/go/src/github.com/HeaInSeo/JUMI log --oneline origin/main..HEAD
git -C /opt/go/src/github.com/HeaInSeo/artifact-handoff log --oneline origin/main..HEAD
git -C /opt/go/src/github.com/HeaInSeo/node-artifact-runtime log --oneline origin/main..HEAD

# 경로 확인
grep "NAN_REPO_ROOT\|AH_REPO_ROOT" /opt/go/src/github.com/HeaInSeo/JUMI/Makefile

# baseline 테스트
cd /opt/go/src/github.com/HeaInSeo/JUMI && make verify-sprint-3d-baseline
```

로컬이 origin보다 앞서 있으면 push 먼저, 뒤처져 있으면 pull 먼저 한다.
