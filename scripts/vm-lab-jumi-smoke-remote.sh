#!/usr/bin/env bash
set -euo pipefail

JUMI_SPEC_PATH="${JUMI_SPEC_PATH:-/home/ubuntu/jumi-handoff-smoke.json}"
JUMI_SMOKE_BIN="${JUMI_SMOKE_BIN:-/home/ubuntu/jumi-smoke}"
JUMI_GRPC_ADDR="${JUMI_GRPC_ADDR:-127.0.0.1:19090}"
JUMI_NAMESPACE="${JUMI_NAMESPACE:-batch-int-dev}"
JUMI_SERVICE="${JUMI_SERVICE:-svc/jumi}"
PORT_FORWARD_PORT="${PORT_FORWARD_PORT:-19090}"
JUMI_RUN_ID="${JUMI_RUN_ID:-}"
JUMI_SAMPLE_RUN_ID="${JUMI_SAMPLE_RUN_ID:-}"
KUBECTL_CMD="${KUBECTL_CMD:-sudo kubectl}"
JUMI_SMOKE_TIMEOUT="${JUMI_SMOKE_TIMEOUT:-}"
JUMI_SMOKE_POLL="${JUMI_SMOKE_POLL:-}"

read -r -a kubectl_cmd <<<"${KUBECTL_CMD}"

chmod +x "${JUMI_SMOKE_BIN}"

"${kubectl_cmd[@]}" -n "${JUMI_NAMESPACE}" port-forward "${JUMI_SERVICE}" "${PORT_FORWARD_PORT}:9090" >/tmp/jumi-smoke-pf.log 2>&1 &
PF_PID=$!
cleanup() {
  kill "${PF_PID}" >/dev/null 2>&1 || true
  wait "${PF_PID}" >/dev/null 2>&1 || true
}
trap cleanup EXIT

for _ in $(seq 1 20); do
  if grep -q "Forwarding from" /tmp/jumi-smoke-pf.log 2>/dev/null; then
    break
  fi
  sleep 1
done
if ! grep -q "Forwarding from" /tmp/jumi-smoke-pf.log 2>/dev/null; then
  echo "port-forward did not become ready" >&2
  cat /tmp/jumi-smoke-pf.log >&2 || true
  exit 1
fi

args=(
  -addr "${JUMI_GRPC_ADDR}"
  -spec "${JUMI_SPEC_PATH}"
)
if [[ -n "${JUMI_RUN_ID}" ]]; then
  args+=(-run-id "${JUMI_RUN_ID}")
fi
if [[ -n "${JUMI_SAMPLE_RUN_ID}" ]]; then
  args+=(-sample-run-id "${JUMI_SAMPLE_RUN_ID}")
fi
if [[ -n "${JUMI_SMOKE_TIMEOUT}" ]]; then
  args+=(-timeout "${JUMI_SMOKE_TIMEOUT}")
fi
if [[ -n "${JUMI_SMOKE_POLL}" ]]; then
  args+=(-poll "${JUMI_SMOKE_POLL}")
fi
"${JUMI_SMOKE_BIN}" "${args[@]}"
