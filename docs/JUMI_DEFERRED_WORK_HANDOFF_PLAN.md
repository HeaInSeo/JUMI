# Deferred Work Handoff Plan

작성일: 2026-05-31  
최종 갱신: 2026-05-31  
상태: Active Handoff Plan  
목적: 남은 deferred item을 가장 작은 스프린트 단위로 분해해서 다음 agent가 중간에 흐트러지지 않고 이어서 진행할 수 있도록 한다.

---

## 0. 세션 시작 전 점검 체크리스트

**코드부터 시작하지 말 것. 아래를 먼저 확인한다.**

```bash
# 1. 세 레포 모두 origin/main 동기화 확인
git -C /opt/go/src/github.com/HeaInSeo/JUMI        fetch origin
git -C /opt/go/src/github.com/HeaInSeo/artifact-handoff fetch origin
git -C /opt/go/src/github.com/HeaInSeo/node-artifact-runtime fetch origin

git -C /opt/go/src/github.com/HeaInSeo/JUMI        log --oneline origin/main..HEAD
git -C /opt/go/src/github.com/HeaInSeo/artifact-handoff log --oneline origin/main..HEAD
git -C /opt/go/src/github.com/HeaInSeo/node-artifact-runtime log --oneline origin/main..HEAD

# 2. JUMI Makefile 경로 변수 확인 (오래된 /tmp 참조가 없어야 함)
grep "NAN_REPO_ROOT\|AH_REPO_ROOT" /opt/go/src/github.com/HeaInSeo/JUMI/Makefile

# 3. baseline 테스트 통과 확인
cd /opt/go/src/github.com/HeaInSeo/JUMI && make verify-sprint-3d-baseline
```

점검 기준:

- 세 레포 모두 `origin/main..HEAD` 가 비어 있어야 한다 (로컬이 앞서 있으면 push 먼저).
- `NAN_REPO_ROOT` = `/opt/go/src/github.com/HeaInSeo/node-artifact-runtime`
- `AH_REPO_ROOT` = `/opt/go/src/github.com/HeaInSeo/artifact-handoff`
- `verify-sprint-3d-baseline` 이 통과해야 한다.

점검 없이 작업을 시작하면 stale 경로나 미푸시 커밋 위에서 작업하게 된다.  
이전 세션에서 실제로 `/tmp` 경로 방치, 17커밋 뒤처진 AH, 미푸시 커밋 혼재 상황이 있었다.

---

## 1. 현재 완료 기준선

### 스프린트 완료 목록

- Sprint 3A remote_fetch happy path
- Sprint 3B same-node local_reuse happy path
- Sprint 3C-1 source registry 저장 모델
- Sprint 3C-2 candidate planner
- Sprint 3C-3A~3E security / parity / fail-closed hardening
- Sprint 3C-4A node-contract input baseline
- Sprint 3D-1 AddSource lifecycle minimal API
- Sprint 3D-2 source verifier minimal API
- Sprint 3D-3 local verification baseline
- Sprint 3D-4 remote smoke verification baseline
- Sprint 3D-5 sample run lifecycle query seam
- Sprint 3D-6A lifecycle retention visibility baseline ✓

### 인프라 완료 목록 (2026-05-31)

- **nan 경로 정규화**: `/tmp/node-artifact-runtime` → `/opt/go/src/github.com/HeaInSeo/node-artifact-runtime`
- **AH 경로 정규화**: `/tmp/artifact-handoff-3c1` → `/opt/go/src/github.com/HeaInSeo/artifact-handoff`
- **nan 가드레일**: Makefile, `.golangci.yml` (gosec/depguard/errcheck 등), `.gitignore` 추가; `helper.go` 보안 수정 (0o755→0o750, defer 패턴, copyFile 제거)
- **Ko 마이그레이션**: JUMI `Dockerfile` 삭제, AH `Containerfile`/`devspace.yaml`/`Dockerfile` 삭제
- **Ko 베이스 이미지**: JUMI + AH 모두 `gcr.io/distroless/static-debian12:nonroot`
- **AH Ko 스크립트**: `scripts/preflight-ko-remote.sh`, `scripts/publish-ah-resolver-ko-remote.sh`, Makefile `ko-publish-remote` 타겟 추가
- **smoke 스크립트**: AH 이미지 빌드를 `podman build` → Ko로 교체 (두 smoke 스크립트 모두)

### 잔존 항목 (의도적 deferred)

- JUMI `Containerfile`: nan NodeForge 이후 삭제 예정 (RUNTIME_SHORTCUT_IMAGE 대체 전까지 유지)
- JUMI `devspace.yaml`: JUMI Containerfile 삭제 이후 정리
- nan Ko 빌드: NodeForge에서 처리 예정

### 현재 정본 기준

- JUMI `main` @ GitHub
- artifact-handoff `main` @ GitHub
- node-artifact-runtime `main` @ GitHub
- 브랜치 정책: **단일 브랜치 (main only)**

---

## 2. 아직 남은 deferred item

1. post-scheduling `ResolveBinding(targetNodeName=actualNode)`
2. signed URL 지원 정책과 runtime-only 처리
3. `local_reuse` zero-copy 전략
4. 새 backend
5. full CI wiring
6. cleanup / TTL / eviction controller (3D-6A는 visibility baseline만; 실제 삭제/루프는 미포함)

---

## 3. 권장 진행 순서

### Sprint 3E

이름: `post-scheduling ResolveBinding minimum`

이유:
- 가장 큰 구조 변경이다.
- lifecycle visibility baseline(3D-6A)이 닫혔으므로 이제 진행 가능하다.

이번 스프린트 최소 목표:
- actual scheduled node 관찰
- `ResolveBinding(targetNodeName=actualNode)` 재호출 seam 추가
- fallback 선택 자체는 최소 범위로 제한

이번 스프린트에서 하지 말 것:
- multi-stage retry loop 확대
- new backend fallback
- large refactor

### Sprint 3F

이름: `signed URL policy`

최소 목표:
- 현재 query reject 정책 이후의 signed URL 처리 방침 확정
- metadata 저장 금지 / runtime-only 허용 여부 결정

### Sprint 3G

이름: `local_reuse strategy decision`

최소 목표:
- `copy default` 유지 여부 재확인
- `reflink optional`
- `hardlink explicit opt-in`
- `copy fallback`

### Sprint 3H

이름: `CI wiring baseline`

최소 목표:
- 이미 있는 verification command를 CI job으로 연결
- Harbor/GHCR sync smoke를 manual gate 또는 scheduled gate로 연결

---

## 4. 다음 agent가 바로 시작할 작업

### Start Here

`Sprint 3E | post-scheduling ResolveBinding minimum`

0번 점검 체크리스트를 먼저 실행하고, 통과 후 작업을 시작한다.

시작 전 확인:
- `make verify-sprint-3d-baseline` 통과
- `bin/jumi-lifecycle` 빌드 가능 (`make lifecycle-check-build`)

---

## 5. scope creep 방지

다음 agent는 아래를 한 스프린트에 섞지 않는다.

- post-scheduling re-resolve와 cleanup controller를 같이 하지 않음
- signed URL 정책과 new backend를 같이 하지 않음
- zero-copy 전략과 CI wiring을 같이 하지 않음
- actual artifact deletion은 visibility baseline에 포함하지 않음

---

## 6. 운영 메모

- Harbor primary / GHCR backup sync 정책은 유지한다.
- smoke와 `k8sgpt` lint는 현재 기준선을 깨지 않는 선에서만 확장한다.
- node-contract canonical input baseline은 이미 올라가 있으므로, 새 작업은 가능하면 contract 기준으로 붙인다.
- Ko 베이스 이미지 변경은 `.ko.yaml` 한 줄 수정으로 가능하다. CGO_ENABLED=0 유지하는 한 `distroless/static-debian12:nonroot` 유지.
- SQLite 도입 시 CGo-free 드라이버(`modernc.org/sqlite`) 사용 — 그래야 `static` 이미지 유지 가능.

---

## 7. 한 줄 결론

3D-6A는 완료됐다. 다음은 `Sprint 3E | post-scheduling ResolveBinding minimum`이다.  
단, 세션 시작 전 반드시 **섹션 0의 점검 체크리스트**를 먼저 실행한다.
