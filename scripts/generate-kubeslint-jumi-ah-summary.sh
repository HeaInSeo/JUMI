#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
TOOL_DIR="${ROOT_DIR}/tools/kubeslint-smoke-summary"
FIXTURE_PATH="${FIXTURE_PATH:-${ROOT_DIR}/deploy/devspace/fixtures/kube-slint-jumi-ah-smoke-metrics.json}"
OUTPUT_PATH="${OUTPUT_PATH:-${ROOT_DIR}/artifacts/jumi-ah-smoke-sli-summary.json}"
PROFILE="${PROFILE:-smoke}"
GOCACHE_DIR="${GOCACHE_DIR:-/tmp/jumi-kubeslint-gocache}"
GOTMPDIR_DIR="${GOTMPDIR_DIR:-/tmp/jumi-kubeslint-gotmp}"
GOMODCACHE_DIR="${GOMODCACHE_DIR:-/tmp/jumi-kubeslint-gomodcache}"
GO_TOOLCHAIN="${GO_TOOLCHAIN:-local}"
GO_BIN="${GO_BIN:-}"
GO_ROOT_OVERRIDE="${GO_ROOT_OVERRIDE:-}"

if [[ -z "${GO_BIN}" ]]; then
  if [[ -x /opt/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.25.10.linux-amd64/bin/go ]]; then
    GO_BIN="/opt/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.25.10.linux-amd64/bin/go"
  else
    GO_BIN="go"
  fi
fi

if [[ -z "${GO_ROOT_OVERRIDE}" ]]; then
  case "${GO_BIN}" in
    /opt/go/pkg/mod/golang.org/toolchain@*/bin/go)
      GO_ROOT_OVERRIDE="$(cd "$(dirname "${GO_BIN}")/.." && pwd)"
      ;;
  esac
fi

mkdir -p "$(dirname "${OUTPUT_PATH}")"
mkdir -p "${GOCACHE_DIR}" "${GOTMPDIR_DIR}" "${GOMODCACHE_DIR}"

(
  cd "${TOOL_DIR}"
  env \
    GOTOOLCHAIN="${GO_TOOLCHAIN}" \
    GOCACHE="${GOCACHE_DIR}" \
    GOTMPDIR="${GOTMPDIR_DIR}" \
    GOMODCACHE="${GOMODCACHE_DIR}" \
    ${GO_ROOT_OVERRIDE:+GOROOT="${GO_ROOT_OVERRIDE}"} \
    "${GO_BIN}" run . -in "${FIXTURE_PATH}" -out "${OUTPUT_PATH}" -profile "${PROFILE}"
)
