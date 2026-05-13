#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
TOOL_DIR="${ROOT_DIR}/tools/kubeslint-smoke-summary"
FIXTURE_PATH="${FIXTURE_PATH:-${ROOT_DIR}/deploy/devspace/fixtures/kube-slint-jumi-ah-smoke-metrics.json}"
OUTPUT_PATH="${OUTPUT_PATH:-${ROOT_DIR}/artifacts/jumi-ah-smoke-sli-summary.json}"
PROFILE="${PROFILE:-smoke}"

mkdir -p "$(dirname "${OUTPUT_PATH}")"

(
  cd "${TOOL_DIR}"
  go run . -in "${FIXTURE_PATH}" -out "${OUTPUT_PATH}" -profile "${PROFILE}"
)
