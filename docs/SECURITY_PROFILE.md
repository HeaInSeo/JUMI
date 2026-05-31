# Security Profile

작성일: 2026-05-31  
적용 범위: JUMI · artifact-handoff (jumi-ah-dev namespace)

---

## 현재 프로파일: mesh-internal transport

JUMI와 AH는 서비스 메시(Istio / Linkerd) 환경에서 동작하는 것을 전제로 한다.

### 보안 계층

| 계층 | 구현 | 위치 |
|---|---|---|
| 네트워크 격리 | NetworkPolicy (default-deny-all + 명시적 허용) | `deploy/k8s/network-policy.yaml` |
| Transport 암호화 | 메시 사이드카 mTLS (자동) | 메시 컨트롤 플레인 |
| 서비스 ID 인증 | AuthorizationPolicy (SPIFFE/SVID 기반) | 메시 컨트롤 플레인 |
| 앱 레벨 TLS | **미구현** — future-profile 참조 | — |

### NetworkPolicy 요약

```
external → jumi:8080       (operator HTTP API)
jumi     → artifact-handoff:8080/9090
jumi     → kube-apiserver:443/6443
*        → kube-dns:53
```

AH는 jumi에서만 수신한다. 외부에서 AH에 직접 접근하는 경로는 없다.

### 코드 마커

`cmd/jumi/main.go`의 `grpc.NewServer()` 호출 위에 아래 주석이 있다:

```go
// mesh-internal: transport security (mTLS) is handled by the sidecar proxy.
// future-profile: add app-level TLS via grpc.Creds(credentials.NewTLS(...))
// when operating outside a service mesh.
```

---

## Future Profile: app-level TLS

메시 없이 운영해야 하는 경우 (예: on-prem, 메시 미지원 환경):

1. `grpc.NewServer(grpc.Creds(credentials.NewTLS(tlsConfig)))` 으로 서버 TLS 추가
2. 클라이언트도 `grpc.WithTransportCredentials(credentials.NewTLS(...))` 설정
3. 인증서 rotation 정책 수립 (cert-manager 또는 수동)
4. 이 변경 전 `DEVELOPMENT_POLICY.md` 의 "결정이 필요한 것은 먼저 묻는다" 절차 따를 것

**app-level TLS는 현재 구현되지 않는다.**

---

## go-grpc-kit 재평가 조건

`github.com/HeaInSeo/go-grpc-kit` 도입을 재검토하려면:

1. `viper` 의존성 제거 또는 opt-in으로 분리
2. `Server()` 메서드 TODO 해소
3. `go.mod`의 grpc 버전과 buf 버전 일치 확인
4. `DEVELOPMENT_POLICY.md` "외부 의존성 추가" 절차에 따라 승인 후 도입
