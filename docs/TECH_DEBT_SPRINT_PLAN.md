# 기술부채 감사 결과 및 스프린트 계획

작성일: 2026-05-31  
최종 갱신: 2026-05-31  
감사 범위: JUMI · artifact-handoff · node-artifact-runtime  
상태: 활성

---

## 세션 재진입 가이드

이 문서로 이어서 작업할 경우:

1. `JUMI_DEFERRED_WORK_HANDOFF_PLAN.md` 섹션 0 점검 체크리스트 먼저 실행
2. 아래 **P0 스프린트 계획**에서 `[ ] 대기 중` 상태인 첫 번째 스프린트부터 시작
3. 각 항목 완료 시 `[ ]` → `[x]` 로 변경 후 커밋
4. 스프린트 전체 완료 시 스프린트 상태 줄도 `[x] 완료 (날짜, commit: 해시)` 로 업데이트

---

## 1. P0 전체 항목

### node-artifact-runtime (nan)

- [ ] **nan-P0-1** `helper.go:674` SSRF — HTTP source URL에 RFC1918 사설 IP 미차단 (`net.IP.IsPrivate()` 추가)
- [ ] **nan-P0-2** `helper.go:705` SSRF — DNS rebinding 방어 없음 (resolve 후 IP 재검증 미적용)
- [ ] **nan-P0-3** `helper.go:872` 보안 — CAS output symlink 검증 없음 (컨테이너 외부 경로 쓰기 가능)
- [ ] **nan-P0-4** `helper.go:308` 버그 — `AllowDirectoryOutput` 조건 논리 반전

### JUMI

- [ ] **jumi-P0-1** `executor.go:158` 버그 — Cancel이 `BuildingBindings/ResolvingInputs/Starting` 상태에서 k8s Job 미정리 (zombie 누적)
- [ ] **jumi-P0-2** `executor.go:196,199` 버그 — `finalizeRun` 에러 완전 무시 (run 상태 미확정)
- [~] **jumi-P0-3** `main.go:65` 보안 — gRPC 서버 TLS/인증 없음 → **mesh-internal profile로 대체 결정 (2026-05-31). 앱 레벨 TLS 미구현. 보안 기준: NetworkPolicy + mesh mTLS + AuthorizationPolicy.**
- [ ] **jumi-P0-4** `spawner_k8s.go:1274` 버그 — 63자 truncation 후 trailing dash → k8s Job 이름 invalid

### artifact-handoff (AH)

- [ ] **ah-P0-1** `main.go:73` 데이터 무결성 — `grpcServer.GracefulStop()` 무제한 대기 → SIGKILL 시 SQLite WAL 손상
- [ ] **ah-P0-2** `service.go:897` 데이터 무결성 — `FinalizeSampleRun` 재호출 시 `RetentionUntil` 덮어씀 (GC 무기한 지연)

---

## 2. P0 스프린트 계획

### Sprint P0-A: AH 데이터 무결성

**상태:** `[ ] 대기 중`  
**대상 레포:** artifact-handoff  
**이유:** 가장 빠르게 수정 가능하고 데이터 손실 위험이 즉각적  

**항목:**

- [ ] ah-P0-1: `grpcServer.GracefulStop()` timeout 추가
- [ ] ah-P0-2: `FinalizeSampleRun` idempotency 보장

**수정 내용:**

```go
// ah-P0-1: cmd/artifact-handoff-resolver/main.go
// grpcServer.GracefulStop() 단독 호출 → timeout 래핑으로 교체
done := make(chan struct{})
go func() { grpcServer.GracefulStop(); close(done) }()
select {
case <-done:
case <-time.After(5 * time.Second):
    grpcServer.Stop()
}

// ah-P0-2: pkg/resolver/service.go FinalizeSampleRunCore 상단
// lifecycle 조회 후 이미 finalized면 조기 반환
if lifecycle.Finalized {
    return nil
}
```

**완료 조건:** 두 항목 커밋 + push, `make verify-sprint-3d-baseline` 통과

---

### Sprint P0-B: JUMI executor 버그

**상태:** `[ ] 대기 중` (P0-A 완료 후 시작)  
**대상 레포:** JUMI  

**항목:**

- [ ] jumi-P0-1: Cancel에 `BuildingBindings/ResolvingInputs/Starting` 상태 처리 추가 (Job 정리 or 마킹)
- [ ] jumi-P0-2: `finalizeRun` 에러 처리 — 로그 + run 상태를 Failed로 기록
- [ ] jumi-P0-4: trailing dash trim — `strings.TrimRight(name, "-")` 추가 (`spawner_k8s.go:1274`)

**완료 조건:** 세 항목 커밋 + push, `make verify-sprint-3d-baseline` 통과

---

### Sprint P0-C: nan 보안 하드닝

**상태:** `[ ] 대기 중` (P0-B 완료 후 시작)  
**대상 레포:** node-artifact-runtime  

**항목 (순서 준수):**

- [ ] nan-P0-4: `AllowDirectoryOutput` 조건 반전 수정 (단순 논리 버그, 먼저 수정)
- [ ] nan-P0-1: `helper.go:674` — RFC1918 IP 차단

  ```go
  if ip := net.ParseIP(host); ip != nil {
      if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsPrivate() {
          return fmt.Errorf("http source host %q is not allowed", host)
      }
  }
  ```

- [ ] nan-P0-3: `helper.go:872` — CAS symlink escape 검증 (filepath.EvalSymlinks + HasPrefix 체크)
- [ ] nan-P0-2: `helper.go:705` — DNS rebinding 방어 (dial 시 resolved IP를 `IsPrivate()` 재검증)

**완료 조건:** 네 항목 커밋 + push, nan golangci-lint 통과

---

### Sprint P0-D: JUMI gRPC 보안 프로파일 문서화

**상태:** `[ ] 대기 중`  
**대상 레포:** JUMI  

**결정 사항 (2026-05-31 확정):**

| 항목 | 결정 |
|---|---|
| 기본 보안 모델 | mesh-internal transport profile |
| 앱 레벨 TLS | 미구현 — future profile 설계만 남김 |
| 보안 기준 | NetworkPolicy + mesh mTLS + AuthorizationPolicy |
| gRPC 라이브러리 | 표준 grpc-go 직접 사용 (go-grpc-kit 미도입) |
| go-grpc-kit 재평가 | 별도 정리(viper 의존성 제거, TODO 해소) 후 재검토 |

**이 스프린트의 실제 작업:**

- [ ] `deploy/k8s/` 에 NetworkPolicy 매니페스트 추가 (JUMI → AH gRPC 포트 허용, 외부 ingress 차단)
- [ ] JUMI gRPC 서버 코드(`main.go:65`)에 `// mesh-internal: TLS handled by sidecar proxy` 주석 + `// future-profile: app-level TLS` TODO
- [ ] `docs/SECURITY_PROFILE.md` 에 보안 프로파일 결정 문서화

**완료 조건:** 세 항목 커밋 + push

**⚠️ 앱 레벨 TLS는 이 스프린트에서 구현하지 않는다.**

---

## 3. P1 항목 (P0 완료 후 진행)

### node-artifact-runtime (nan)

- [ ] **nan-P1-1** `InspectOnSuccessOnly` 미구현 — 항상 전체 inspect 실행
- [ ] **nan-P1-2** `FailOnMissingRequiredOutput` 조건 반전 버그
- [ ] **nan-P1-3** `http.DefaultTransport` type assertion panic 가능
- [ ] **nan-P1-4** `commandEnv` 내부 에러 silent 무시
- [ ] **nan-P1-5** 전체 실행 타임아웃 없음 — 무한 실행 가능
- [ ] **nan-P1-6** symlink TOCTOU (check→use 사이 교체 가능)

### JUMI

- [ ] **jumi-P1-1** `RetryPolicy.MaxAttempts` 미구현
- [ ] **jumi-P1-2** `Defaults` 필드 미적용
- [ ] **jumi-P1-3** `FailurePolicy.Mode` 무시 — 항상 fail-fast
- [ ] **jumi-P1-4** 취소된 ctx를 `CancelNode`에 전달 → zombie Job 가능
- [ ] **jumi-P1-5** `/readyz` 항상 200 반환
- [ ] **jumi-P1-6** `WaitNode` 에러 결과 신뢰 문제

### artifact-handoff (AH)

- [ ] **ah-P1-1** `main.go:21` 기본 DSN=`"memory"` — env 없으면 무음 데이터 손실 (경고 로그 추가)
- [ ] **ah-P1-2** `sqlite.go:57` SQLite 핵심 컬럼 인덱스 없음 (artifact_id, sample_run_id 등)
- [ ] **ah-P1-3** `grpc.go` 모든 gRPC 에러가 `codes.Unknown` — 클라이언트 retry 오작동
- [ ] **ah-P1-4** `http.go` HTTP POST body 크기 제한 누락 — OOM 가능
- [ ] **ah-P1-5** `service.go:808` 사설 IP(`10.x`, `172.16-31.x`, `192.168.x`) 미차단 — SSRF
- [ ] **ah-P1-6** `ids.go:12` 키 구분자 `/` 인젝션 → 키 충돌
- [ ] **ah-P1-7** `sqlite.go:57` SQLite 마이그레이션 비트랜잭션 실행
- [ ] **ah-P1-8** `main.go:34` HTTP `ReadTimeout`/`WriteTimeout` 없음 (Slowloris)

---

## 4. P2 항목 (기술부채)

### node-artifact-runtime (nan)

- [ ] **nan-P2-1** `ContainerName` 플래그 미바인딩
- [ ] **nan-P2-2** `firstNonEmpty` 중복 (JUMI에도 동일 코드 3곳)
- [ ] **nan-P2-3** termination log 쓰기 에러 무시
- [ ] **nan-P2-4** `safeInputName` 길이 제한 없음
- [ ] **nan-P2-5** env key suffix 파싱 충돌 가능성
- [ ] **nan-P2-6** manifest dir symlink 검증 없음

### JUMI

- [ ] **jumi-P2-1** `/statusz` O(N×M) registry 쿼리
- [ ] **jumi-P2-2** `wrapped-shell`, `runtime-helper` 레거시 미제거
- [ ] **jumi-P2-3** `MemoryRegistry` 재시작 시 상태 손실
- [ ] **jumi-P2-4** 5초 shutdown이 in-flight goroutine 강제 종료
- [ ] **jumi-P2-5** `GetRun`/`ListRunNodes` runID 검증 없음
- [ ] **jumi-P2-6** HTTP 상태코드를 에러 문자열 파싱으로 추출
- [ ] **jumi-P2-7** 10ms polling GC pressure
- [ ] **jumi-P2-8** `firstNonEmpty` 3곳에 중복

### artifact-handoff (AH)

- [ ] **ah-P2-1** `service.go:867` `hasAnyNodeLocalSource` dead code
- [ ] **ah-P2-2** `service.go:534` unreachable 분기 dead code
- [ ] **ah-P2-3** `SourceID` SHA-256 중 8바이트만 사용 (64비트 → birthday collision)
- [ ] **ah-P2-4** `isDuplicateColumn` 에러 메시지 하드코딩
- [ ] **ah-P2-5** gRPC 서버 옵션 미설정 (MaxRecvMsgSize, keepalive 기본값 의존)
- [ ] **ah-P2-6** k8s Deployment resource limits 없음

---

## 5. 완료된 스프린트

| 스프린트 | 완료일 | 커밋 |
|---|---|---|
| (없음 — 2026-05-31 감사 이후 아직 시작 안 함) | | |
