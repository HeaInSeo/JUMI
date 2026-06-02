# Deferred Work Handoff Plan

작성일: 2026-05-31  
최종 갱신: 2026-05-31  
상태: Active Handoff Plan  
목적: 남은 deferred item을 가장 작은 스프린트 단위로 분해해서 다음 agent가 중간에 흐트러지지 않고 이어서 진행할 수 있도록 한다.

개발 정책 문서: `/opt/go/src/github.com/HeaInSeo/DEVELOPMENT_POLICY.md`

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
- Sprint 3E post-scheduling ResolveBinding minimum ✓
- Sprint 3F signed URL policy ✓
- Sprint 3G local_reuse strategy decision ✓
- Sprint 3H CI wiring baseline ✓
- Sprint 3I new backend scope decision ✓
- Sprint 3J cleanup TTL eviction scope decision ✓
- Sprint 3K direct Kubernetes backend boundary ✓
- Sprint 3L Kubernetes churn risk boundary ✓
- Sprint 3M Kubernetes churn observability baseline ✓

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

현재 JUMI/AH/nan 트랙에서 즉시 진행할 deferred item은 없다.

조건부 deferred:

1. `jumi-P2-2`: nan runtime image가 전체 노드에 롤아웃된 이후 `spawner_k8s.go`의 wrapped-shell/runtime-helper compatibility branch 제거
2. bori operator track: cleanup / TTL / eviction controller의 실제 삭제/조정 루프
3. bori/operator track: 대량 유전체 workload의 Kubernetes object/API/etcd/node-local filesystem churn reconciliation

---

## 3. 권장 진행 순서

### Sprint 3E

이름: `post-scheduling ResolveBinding minimum`

상태: 완료 (2026-06-01)

완료 내용:
- scheduled Pod의 actual node(`PodNodeName`) 관찰
- artifact binding별 `ResolveBinding(targetNodeName=actualNode)` 재호출 seam 추가
- post-scheduling resolve 결과를 `node.input_post_scheduling_resolved` 이벤트로 기록
- 재호출 실패는 실행 중인 Pod를 실패시키지 않고 `node.input_post_scheduling_resolve_failed` 경고 이벤트로 기록
- fallback 선택/Pod runtime context 재작성은 범위 밖으로 유지

검증:
- `go test ./pkg/executor`
- `make verify-sprint-3d-baseline`

### Sprint 3F

이름: `signed URL policy`

상태: 완료 (2026-06-01)

완료 내용:
- AH persistent metadata에는 signed URL/query-bearing HTTP URI를 저장하지 않기로 고정
- nan runtime input URI도 signed URL/query-bearing URI를 직접 실행하지 않기로 고정
- future 지원은 `credentialRef` 또는 materialization broker가 실행 직전에 short-lived URL을 발급하는 runtime-only flow로 제한
- direct signed URL pass-through는 미지원으로 명시

검증:
- `artifact-handoff`: `go test ./pkg/domain ./pkg/resolver`
- `node-artifact-runtime`: `go test ./pkg/runtimehelper`

### Sprint 3G

이름: `local_reuse strategy decision`

상태: 완료 (2026-06-01)

완료 내용:
- `local_reuse` 기본 materialization strategy는 `copy`로 유지
- `reflink`는 future optional optimization으로만 허용
- `hardlink`는 future explicit opt-in으로만 허용
- zero-copy 계열 최적화 실패 시 copy fallback을 필수로 결정
- symlink 기반 materialization은 계속 금지
- nan test로 materialized input이 source artifact와 같은 파일이 아니며 consumer mutation이 source를 오염시키지 않음을 고정

검증:
- `node-artifact-runtime`: `go test ./pkg/runtimehelper`

### Sprint 3H

이름: `CI wiring baseline`

상태: 완료 (2026-06-01)

완료 내용:
- JUMI `.github/workflows/sprint-baseline.yml` 추가
- GitHub Actions에서 JUMI / artifact-handoff / node-artifact-runtime을 같은 workspace에 checkout
- `make verify-sprint-3d-baseline`을 cross-repo CI baseline으로 연결
- Harbor/GHCR sync smoke는 `.github/workflows/registry-sync-smoke.yml`의 manual `workflow_dispatch` gate로 연결
- 수동 gate는 `preflight-only`, `local-reuse`, `remote-fetch`, `full` 모드를 제공

검증:
- `make verify-sprint-3d-baseline`
- `go build ./cmd/jumi`

### Sprint 3I

이름: `new backend scope decision`

상태: 완료 (2026-06-01)

결정 문서:
- [JUMI_NEW_BACKEND_SCOPE_DECISION.md](./JUMI_NEW_BACKEND_SCOPE_DECISION.md)

완료 내용:
- "새 backend"를 AH transport backend나 bori operator가 아니라 JUMI execution backend 정리로 정의
- JUMI / AH / nan / bori 책임 경계 확정
- `pkg/backend/spawner_k8s.go`의 direct Kubernetes/nan path와 spawner compatibility path 분리 방향 확정
- `backend.Adapter`는 구체적 API gap이 확인되기 전까지 유지
- `jumi-P2-2` precondition 충족 전 legacy wrapped-shell/runtime-helper 경로는 유지

### Sprint 3J

이름: `cleanup TTL eviction scope decision`

상태: 완료 (2026-06-01)

결정 문서:
- [JUMI_CLEANUP_TTL_EVICTION_SCOPE_DECISION.md](./JUMI_CLEANUP_TTL_EVICTION_SCOPE_DECISION.md)

완료 내용:
- Kubernetes Job cleanup은 현행 `ttlSecondsAfterFinished` 전달로 유지
- AH는 lifecycle finalization / retention window / GC eligibility / backlog visibility까지 담당
- JUMI는 node-local artifact cache나 AH metadata를 직접 삭제하지 않음
- 실제 eviction/reconciliation loop는 bori operator track으로 이관
- AH deletion/tombstone API와 node-local safe-delete component 설계 전 JUMI cleanup controller는 만들지 않음

### Sprint 3K

이름: `direct Kubernetes backend boundary`

상태: 완료 (2026-06-01)

완료 내용:
- `SpawnerK8sAdapter` 내부의 direct Kubernetes/nan 실행 path를 `directK8sBackend` 경계로 분리
- direct Start / Wait / Cancel 동작을 해당 경계로 위임
- 기존 spawner compatibility path와 `backend.Adapter` 인터페이스는 유지
- direct backend Start가 Kubernetes Job을 생성하고 serviceAccount/workingDir를 보존하는 테스트 추가

검증:
- `go test ./pkg/backend`
- `make verify-sprint-3d-baseline`

### Sprint 3L

이름: `Kubernetes churn risk boundary`

상태: 완료 (2026-06-02)

결정 문서:
- [JUMI_K8S_CHURN_RISK_BOUNDARY.md](./JUMI_K8S_CHURN_RISK_BOUNDARY.md)

완료 내용:
- client-go 기반 동적 Job 생성 구조가 production-scale genomic workload churn을 닫지 못한다는 점을 명시
- K8s Job/Pod object churn, API server/etcd pressure, in-memory state recovery, node-local filesystem churn, artifact metadata churn 위험을 분리
- 현재 JUMI guardrail은 bounded release / Job TTL / fast-fail / AH lifecycle visibility 수준임을 고정
- 실제 churn reconciliation은 bori/operator track으로 이관
- executable fixture smoke test가 authored pipeline end-to-end production readiness를 의미하지 않는다고 명시

### Sprint 3M

이름: `Kubernetes churn observability baseline`

상태: 완료 (2026-06-02)

결정 문서:
- [JUMI_K8S_CHURN_OBSERVABILITY_BASELINE.md](./JUMI_K8S_CHURN_OBSERVABILITY_BASELINE.md)

완료 내용:
- live smoke가 K8s Job/Pod churn start/end snapshot을 kube-slint fixture에 기록하도록 기준선 추가
- static replay fixture에 `k8sChurn.start` / `k8sChurn.end` 예시 추가
- kube-slint summary tool에 churn derived SLI 추가
- observation-only Job/Pod namespace delta와 run-labeled Job/Pod count를 분리
- failed/active Job end-state를 최소 debt signal로 정의

---

## 4. 다음 agent가 바로 시작할 작업

### Start Here

`No immediate JUMI/AH/nan deferred sprint`

0번 점검 체크리스트를 먼저 실행하고, 통과 후 작업을 시작한다.

시작 전 확인:
- `make verify-sprint-3d-baseline` 통과
- `cmd/jumi` 빌드 가능 (`env GOROOT=/usr/local/go GOCACHE=/tmp/jumi-gocache GOTMPDIR=/tmp/jumi-gotmp go build ./cmd/jumi`)

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

JUMI/AH/nan의 즉시 진행 deferred sprint는 닫혔다. 3K에서 direct Kubernetes/nan backend decomposition의 첫 경계도 분리했고, 3L/3M에서 production-scale K8s churn 위험과 관측 가능한 최소 기준선을 고정했다.  
남은 조건부 작업은 nan runtime rollout 이후 `jumi-P2-2`, 그리고 bori operator track의 cleanup/eviction/churn reconciliation 설계다.
