SHELL := /bin/bash
.SHELLFLAGS := -o pipefail -c

LOCALBIN := $(CURDIR)/bin
REPORT_DIR := $(CURDIR)/reports
GOCACHE_DIR := /tmp/jumi-gocache
GOTMPDIR_DIR := /tmp/jumi-gotmp
GOMODCACHE_DIR := /tmp/jumi-gomodcache

GOLANGCI_LINT := $(LOCALBIN)/golangci-lint
GOLANGCI_LINT_VERSION := v2.11.3

GOVULNCHECK := $(LOCALBIN)/govulncheck
GOVULNCHECK_VERSION := v1.1.4

GOENV := GOCACHE="$(GOCACHE_DIR)" GOTMPDIR="$(GOTMPDIR_DIR)"
GOENV_SMOKE := GOCACHE="$(GOCACHE_DIR)" GOTMPDIR="$(GOTMPDIR_DIR)" GOMODCACHE="$(GOMODCACHE_DIR)"
AH_PROTO_DIR ?= ../artifact-handoff/api/proto/ahv1
JUMI_AH_PROTO_DIR := $(CURDIR)/pkg/handoff/ahv1

PKGS_ALL := ./...
PKGS_CORE := ./cmd/... ./internal/... ./pkg/...
PKGS_REGRESSION := ./cmd/... ./pkg/executor ./pkg/handoff ./pkg/metrics ./pkg/observe
PKGS_COVER := ./cmd/... ./internal/... ./pkg/...
PKGS_SECURITY := ./cmd/... ./pkg/...
# Explicit coverpkg list: excludes pkg/handoff/ahv1 (generated protobuf, 297 functions all 0%)
PKGS_COVERPKG := github.com/HeaInSeo/JUMI/cmd/jumi,github.com/HeaInSeo/JUMI/internal/util,github.com/HeaInSeo/JUMI/pkg/api,github.com/HeaInSeo/JUMI/pkg/backend,github.com/HeaInSeo/JUMI/pkg/executor,github.com/HeaInSeo/JUMI/pkg/handoff,github.com/HeaInSeo/JUMI/pkg/metrics,github.com/HeaInSeo/JUMI/pkg/observe,github.com/HeaInSeo/JUMI/pkg/provenance,github.com/HeaInSeo/JUMI/pkg/registry,github.com/HeaInSeo/JUMI/pkg/spec
COVERAGE_THRESHOLD := 70

JUMI_BIN := $(LOCALBIN)/jumi
AH_GRPC_TARGET ?=
AH_HTTP_URL ?=
SAMPLE_RUN_ID ?=

.PHONY: test test-regression coverage coverage-check fmt vet lint lint-depguard lint-security vuln vuln-all golangci-lint govulncheck handoff-proto-sync-check smoke-tool-build preflight-publish-local preflight-publish-remote preflight-ko-remote runtime-build-local runtime-check-local runtime-align-check runtime-smoke-remote ko-publish-remote ko-smoke-remote verify-sprint-3d-baseline verify-sprint-3d-remote lifecycle-check

REMOTE_SSH_TARGET ?= seoy@100.123.80.48
REGISTRY_HOST ?= harbor.10.113.24.96.nip.io
RUNTIME_IMAGE_LOCAL_TAG ?= jumi-runtime:dev
AH_REPO_ROOT ?= /opt/go/src/github.com/HeaInSeo/artifact-handoff
NAN_REPO_ROOT ?= /opt/go/src/github.com/HeaInSeo/node-artifact-runtime
PREFLIGHT_PUBLISH_ENV_SCRIPT := $(CURDIR)/scripts/preflight-publish-env.sh
PREFLIGHT_KO_REMOTE_SCRIPT := $(CURDIR)/scripts/preflight-ko-remote.sh
PUBLISH_JUMI_SERVICE_KO_REMOTE_SCRIPT := $(CURDIR)/scripts/publish-jumi-service-ko-remote.sh

test:
	@mkdir -p "$(GOCACHE_DIR)" "$(GOTMPDIR_DIR)"
	$(GOENV) go test $(PKGS_ALL)

test-regression:
	@mkdir -p "$(GOCACHE_DIR)" "$(GOTMPDIR_DIR)"
	$(GOENV) go test $(PKGS_REGRESSION)

coverage:
	@mkdir -p "$(REPORT_DIR)" "$(GOCACHE_DIR)" "$(GOTMPDIR_DIR)"
	$(GOENV) go test $(PKGS_COVER) -coverprofile="$(REPORT_DIR)/cover.out" -covermode=atomic -coverpkg="$(PKGS_COVERPKG)"
	go tool cover -func="$(REPORT_DIR)/cover.out" | tee "$(REPORT_DIR)/coverage.txt"

coverage-check: coverage
	@TOTAL=$$(go tool cover -func="$(REPORT_DIR)/cover.out" | awk '/^total:/{gsub(/%/,""); print int($$3)}'); \
	echo "[jumi] total coverage: $${TOTAL}%"; \
	if [ "$${TOTAL}" -lt "$(COVERAGE_THRESHOLD)" ]; then \
		echo "FAIL: coverage $${TOTAL}% is below threshold $(COVERAGE_THRESHOLD)%"; exit 1; \
	else \
		echo "OK: coverage $${TOTAL}% >= $(COVERAGE_THRESHOLD)%"; \
	fi

fmt:
	go fmt $(PKGS_ALL)

vet:
	@mkdir -p "$(GOCACHE_DIR)" "$(GOTMPDIR_DIR)"
	$(GOENV) go vet $(PKGS_ALL)

golangci-lint:
	@mkdir -p "$(LOCALBIN)"
	@test -x "$(GOLANGCI_LINT)" || bash -c '\
		set -euo pipefail; \
		curl -fsSL "https://api.github.com/repos/golangci/golangci-lint/releases/tags/$(GOLANGCI_LINT_VERSION)" >/dev/null; \
		OS="$$(uname | tr A-Z a-z)"; \
		ARCH="$$(uname -m)"; \
		case "$$ARCH" in x86_64) ARCH=amd64 ;; aarch64|arm64) ARCH=arm64 ;; *) echo "unsupported arch: $$ARCH"; exit 1 ;; esac; \
		VER="$(GOLANGCI_LINT_VERSION)"; \
		VER="$${VER#v}"; \
		FILE="golangci-lint-$$VER-$$OS-$$ARCH.tar.gz"; \
		URL="https://github.com/golangci/golangci-lint/releases/download/$(GOLANGCI_LINT_VERSION)/$$FILE"; \
		SUM_URL="https://github.com/golangci/golangci-lint/releases/download/$(GOLANGCI_LINT_VERSION)/golangci-lint-$$VER-checksums.txt"; \
		TMP="$$(mktemp -d)"; \
		curl -fsSL "$$URL" -o "$$TMP/lint.tgz"; \
		curl -fsSL "$$SUM_URL" -o "$$TMP/checksums.txt"; \
		EXPECTED="$$(awk -v f="$$FILE" "\$$2==f{print \$$1}" "$$TMP/checksums.txt")"; \
		if [ -z "$$EXPECTED" ]; then echo "checksum not found for $$FILE"; exit 1; fi; \
		if command -v sha256sum >/dev/null 2>&1; then \
			ACTUAL="$$(sha256sum "$$TMP/lint.tgz" | awk "{print \$$1}")"; \
		elif command -v shasum >/dev/null 2>&1; then \
			ACTUAL="$$(shasum -a 256 "$$TMP/lint.tgz" | awk "{print \$$1}")"; \
		else \
			echo "no sha256 tool found (sha256sum/shasum)"; exit 1; \
		fi; \
		if [ "$$EXPECTED" != "$$ACTUAL" ]; then echo "checksum mismatch for $$FILE"; exit 1; fi; \
		tar -xzf "$$TMP/lint.tgz" -C "$$TMP"; \
		cp "$$TMP/golangci-lint-$$VER-$$OS-$$ARCH/golangci-lint" "$(GOLANGCI_LINT)"; \
		chmod +x "$(GOLANGCI_LINT)"; \
		rm -rf "$$TMP"'

govulncheck:
	@mkdir -p "$(LOCALBIN)"
	@test -x "$(GOVULNCHECK)" || GOBIN="$(LOCALBIN)" go install golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION)

lint: golangci-lint lint-depguard
	@mkdir -p "$(REPORT_DIR)" "$(GOCACHE_DIR)" "$(GOTMPDIR_DIR)"
	@$(GOENV) $(GOLANGCI_LINT) run $(PKGS_CORE) | tee "$(REPORT_DIR)/lint.txt"

lint-depguard: golangci-lint
	@mkdir -p "$(REPORT_DIR)" "$(GOCACHE_DIR)" "$(GOTMPDIR_DIR)"
	@$(GOENV) $(GOLANGCI_LINT) run --enable-only depguard $(PKGS_CORE) | tee "$(REPORT_DIR)/lint-depguard.txt"

lint-security: golangci-lint
	@mkdir -p "$(REPORT_DIR)" "$(GOCACHE_DIR)" "$(GOTMPDIR_DIR)"
	@echo "[jumi] security scan scope: $(PKGS_SECURITY)" | tee "$(REPORT_DIR)/lint-security-summary.txt"
	@set +e; \
	$(GOENV) $(GOLANGCI_LINT) run --enable-only gosec $(PKGS_SECURITY) \
	| tee "$(REPORT_DIR)/gosec.txt"; \
	echo "gosec_exit=$$?" | tee -a "$(REPORT_DIR)/lint-security-summary.txt"

vuln: govulncheck
	@mkdir -p "$(REPORT_DIR)" "$(GOCACHE_DIR)" "$(GOTMPDIR_DIR)"
	@set +e; \
	$(GOENV) $(GOVULNCHECK) $(PKGS_SECURITY) 2>&1 | tee "$(REPORT_DIR)/govulncheck-core.txt"; \
	echo "govulncheck_core_exit=$$?" | tee "$(REPORT_DIR)/govulncheck-core.summary"

vuln-all: govulncheck
	@mkdir -p "$(REPORT_DIR)" "$(GOCACHE_DIR)" "$(GOTMPDIR_DIR)"
	@set +e; \
	$(GOENV) $(GOVULNCHECK) ./... 2>&1 | tee "$(REPORT_DIR)/govulncheck-all.txt"; \
	echo "govulncheck_all_exit=$$?" | tee "$(REPORT_DIR)/govulncheck-all.summary"

handoff-proto-sync-check:
	test -f "$(AH_REPO_ROOT)/api/proto/ahv1/ah_v1.pb.go"
	test -f "$(AH_REPO_ROOT)/api/proto/ahv1/ah_v1_grpc.pb.go"
	diff -u "$(AH_REPO_ROOT)/api/proto/ahv1/ah_v1.pb.go" "$(JUMI_AH_PROTO_DIR)/ah_v1.pb.go"
	diff -u "$(AH_REPO_ROOT)/api/proto/ahv1/ah_v1_grpc.pb.go" "$(JUMI_AH_PROTO_DIR)/ah_v1_grpc.pb.go"

smoke-tool-build:
	@mkdir -p "$(GOCACHE_DIR)" "$(GOTMPDIR_DIR)" "$(GOMODCACHE_DIR)"
	cd tools/jumi-smoke && $(GOENV_SMOKE) go build ./...

preflight-publish-local:
	"$(PREFLIGHT_PUBLISH_ENV_SCRIPT)"

preflight-publish-remote:
	REMOTE_SSH_TARGET="$(REMOTE_SSH_TARGET)" REGISTRY_HOST="$(REGISTRY_HOST)" "$(PREFLIGHT_PUBLISH_ENV_SCRIPT)" --remote

preflight-ko-remote:
	REMOTE_SSH_TARGET="$(REMOTE_SSH_TARGET)" REGISTRY_HOST="$(REGISTRY_HOST)" "$(PREFLIGHT_KO_REMOTE_SCRIPT)"

runtime-build-local:
	podman build -f Containerfile -t "$(RUNTIME_IMAGE_LOCAL_TAG)" .

runtime-check-local:
	podman run --rm --entrypoint /usr/local/bin/nan "$(RUNTIME_IMAGE_LOCAL_TAG)" version

runtime-align-check:
	./scripts/check-runtime-alignment.sh

runtime-smoke-remote: preflight-publish-remote
	./scripts/run-jumi-ah-dev-live-smoke-eval.sh

ko-publish-remote: preflight-ko-remote
	REMOTE_SSH_TARGET="$(REMOTE_SSH_TARGET)" REGISTRY_HOST="$(REGISTRY_HOST)" "$(PUBLISH_JUMI_SERVICE_KO_REMOTE_SCRIPT)"

ko-smoke-remote: ko-publish-remote
	REMOTE_JUMI_REPO_ROOT="/tmp/jumi-runtime-refresh" ALLOW_LOCAL_CHECKOUT_FALLBACK=true ./scripts/run-jumi-ah-dev-live-smoke-eval.sh

verify-sprint-3d-baseline: runtime-align-check handoff-proto-sync-check
	@mkdir -p "$(GOCACHE_DIR)" "$(GOTMPDIR_DIR)" /tmp/ah-addsource-tmp /tmp/ah-addsource-cache /tmp/nan-node-contract-tmp /tmp/nan-node-contract-cache
	$(GOENV) go test ./pkg/backend ./pkg/executor ./pkg/handoff
	test -d "$(AH_REPO_ROOT)"
	cd "$(AH_REPO_ROOT)" && env TMPDIR=/tmp/ah-addsource-tmp GOCACHE=/tmp/ah-addsource-cache go test ./pkg/domain ./pkg/inventory ./pkg/resolver
	test -d "$(NAN_REPO_ROOT)"
	cd "$(NAN_REPO_ROOT)" && env TMPDIR=/tmp/nan-node-contract-tmp GOCACHE=/tmp/nan-node-contract-cache go test ./pkg/contract ./pkg/runtimehelper ./cmd/node-artifact-runtime

verify-sprint-3d-remote:
	env SYNC_BACKUP_REGISTRY=true K8SGPT_MODE=required ./scripts/run-jumi-same-node-local-reuse-live-smoke.sh
	env SYNC_BACKUP_REGISTRY=true K8SGPT_MODE=required ENABLE_HTTP_AH=1 ./scripts/run-jumi-remote-fetch-simple-http-live-smoke.sh

lifecycle-check:
	@mkdir -p "$(LOCALBIN)" "$(GOCACHE_DIR)" "$(GOTMPDIR_DIR)"
	$(GOENV) go build -o "$(JUMI_BIN)" ./cmd/jumi
	@if [ -z "$(SAMPLE_RUN_ID)" ]; then echo "usage: make lifecycle-check SAMPLE_RUN_ID=<id> AH_GRPC_TARGET=<host:port>"; echo "       make lifecycle-check SAMPLE_RUN_ID=<id> AH_HTTP_URL=<url>"; exit 1; fi
	@if [ -n "$(AH_GRPC_TARGET)" ]; then \
		"$(JUMI_BIN)" lifecycle-check --ah-grpc="$(AH_GRPC_TARGET)" --sample-run-id="$(SAMPLE_RUN_ID)"; \
	elif [ -n "$(AH_HTTP_URL)" ]; then \
		"$(JUMI_BIN)" lifecycle-check --ah-http="$(AH_HTTP_URL)" --sample-run-id="$(SAMPLE_RUN_ID)"; \
	else \
		echo "error: AH_GRPC_TARGET or AH_HTTP_URL required"; exit 1; \
	fi
