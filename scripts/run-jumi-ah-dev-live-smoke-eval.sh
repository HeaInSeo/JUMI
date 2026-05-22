#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
REMOTE_SSH_TARGET="${REMOTE_SSH_TARGET:-seoy@100.123.80.48}"
REMOTE_KUBECONFIG="${REMOTE_KUBECONFIG:-/opt/go/src/github.com/HeaInSeo/infra-lab/kubeconfig}"
REMOTE_JUMI_REPO_ROOT="${REMOTE_JUMI_REPO_ROOT:-/opt/go/src/github.com/HeaInSeo/JUMI}"
REMOTE_TMP_DIR="${REMOTE_TMP_DIR:-/tmp/jumi-ah-dev-live-smoke}"
VM_NAMESPACE="${VM_NAMESPACE:-jumi-ah-dev}"
JUMI_SERVICE="${JUMI_SERVICE:-svc/jumi}"
ARTIFACT_HANDOFF_SERVICE="${ARTIFACT_HANDOFF_SERVICE:-artifact-handoff}"
JUMI_SMOKE_TOOL_DIR="${JUMI_SMOKE_TOOL_DIR:-${ROOT_DIR}/tools/jumi-smoke}"
FIXTURE_SOURCE_PATH="${FIXTURE_SOURCE_PATH:-${ROOT_DIR}/deploy/devspace/fixtures/jumi-handoff-smoke.json}"
REMOTE_HELPER_PATH="${REMOTE_HELPER_PATH:-${ROOT_DIR}/scripts/vm-lab-jumi-smoke-remote.sh}"
FIXTURE_PATH="${FIXTURE_PATH:-${ROOT_DIR}/artifacts/devspace/kube-slint-jumi-ah-smoke-metrics.live.json}"
SUMMARY_PATH="${SUMMARY_PATH:-${ROOT_DIR}/artifacts/devspace/jumi-ah-smoke-live-sli-summary.json}"
GATE_PATH="${GATE_PATH:-${ROOT_DIR}/artifacts/devspace/gate/slint-gate-live-summary.json}"
POLICY_FILE="${POLICY_FILE:-${ROOT_DIR}/policy/devspace/jumi-ah-live-thresholds.yaml}"
PUBLISH_SHIFT_LEFT_OBSERVABILITY="${PUBLISH_SHIFT_LEFT_OBSERVABILITY:-${PUBLISH_DEV_SPACE:-false}}"
PUBLISH_SHIFT_LEFT_OBSERVABILITY_SCRIPT="${PUBLISH_SHIFT_LEFT_OBSERVABILITY_SCRIPT:-}"
PORT_FORWARD_PORT="${PORT_FORWARD_PORT:-19190}"
SLINT_GATE_BIN="${SLINT_GATE_BIN:-slint-gate}"
SLINT_GATE_MODE="${SLINT_GATE_MODE:-optional}"
ALLOW_LOCAL_CHECKOUT_FALLBACK="${ALLOW_LOCAL_CHECKOUT_FALLBACK:-false}"

RUN_STAMP="$(date -u +%Y%m%dT%H%M%SZ)"
RUN_ID="${RUN_ID:-jumi-ah-dev-live-smoke-${RUN_STAMP}}"
SAMPLE_RUN_ID="${SAMPLE_RUN_ID:-jumi-ah-dev-live-smoke-sample-${RUN_STAMP}}"

JUMI_KEYS=(
  jumi_jobs_created_total
  jumi_artifacts_registered_total
  jumi_input_resolve_requests_total
  jumi_input_remote_fetch_total
  jumi_input_materializations_total
  jumi_sample_runs_finalized_total
  jumi_gc_evaluate_requests_total
)
AH_KEYS=(
  ah_artifacts_registered_total
  ah_resolve_requests_total
  ah_fallback_total
  ah_gc_backlog_bytes
)

require_file() {
  [[ -f "$1" ]] || {
    echo "missing file: $1" >&2
    exit 1
  }
}

need_cmd() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "missing command: $1" >&2
    exit 1
  }
}

log_stage() {
  local stage="$1"
  local result="$2"
  local detail="${3:-}"
  if [[ -n "${detail}" ]]; then
    printf '%s: %s - %s\n' "${stage}" "${result}" "${detail}"
  else
    printf '%s: %s\n' "${stage}" "${result}"
  fi
}

ssh_remote() {
  ssh -F /dev/null -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null "$REMOTE_SSH_TARGET" "$@"
}

scp_remote() {
  scp -F /dev/null -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null "$@"
}

collect_metric_values() {
  local service="$1"
  local prefix="$2"
  local tmp_name="$3"
  ssh_remote "
    export KUBECONFIG='${REMOTE_KUBECONFIG}'
    kubectl -n '${VM_NAMESPACE}' run '${tmp_name}' \
      --image=busybox:1.36 \
      --restart=Never \
      --rm -i \
      --command -- sh -c 'wget -qO- http://${service}:8080/metrics | grep ${prefix}_ || true'
  "
}

collect_service_json() {
  local service="$1"
  local path="$2"
  local tmp_name="$3"
  ssh_remote "
    export KUBECONFIG='${REMOTE_KUBECONFIG}'
    kubectl -n '${VM_NAMESPACE}' run '${tmp_name}' \
      --image=busybox:1.36 \
      --restart=Never \
      --rm -i \
      --command -- sh -c 'wget -qO- http://${service}:8080${path}'
  "
}

need_cmd ssh
need_cmd scp
need_cmd python3

case "${SLINT_GATE_MODE}" in
  skip|optional|required) ;;
  *)
    echo "invalid SLINT_GATE_MODE: ${SLINT_GATE_MODE}" >&2
    exit 1
    ;;
esac

require_file "$FIXTURE_SOURCE_PATH"
require_file "$REMOTE_HELPER_PATH"
require_file "$POLICY_FILE"
[[ -d "${JUMI_SMOKE_TOOL_DIR}" ]] || {
  echo "missing directory: ${JUMI_SMOKE_TOOL_DIR}" >&2
  exit 1
}

mkdir -p "$(dirname "${FIXTURE_PATH}")" "$(dirname "${SUMMARY_PATH}")" "$(dirname "${GATE_PATH}")"

start_jumi="$(mktemp)"
start_ah="$(mktemp)"
end_jumi="$(mktemp)"
end_ah="$(mktemp)"
lifecycle_json="$(mktemp)"
artifacts_json="$(mktemp)"
cleanup() {
  rm -f "${start_jumi}" "${start_ah}" "${end_jumi}" "${end_ah}" "${lifecycle_json}" "${artifacts_json}"
  ssh_remote "rm -rf '${REMOTE_TMP_DIR}'" >/dev/null 2>&1 || true
}
trap cleanup EXIT

started_at="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

ssh_remote "
  rm -rf '${REMOTE_TMP_DIR}' &&
  mkdir -p '${REMOTE_TMP_DIR}' &&
  export KUBECONFIG='${REMOTE_KUBECONFIG}' &&
  kubectl -n '${VM_NAMESPACE}' rollout status deploy/jumi --timeout=180s &&
  kubectl -n '${VM_NAMESPACE}' rollout status deploy/${ARTIFACT_HANDOFF_SERVICE} --timeout=180s
"
scp_remote "${FIXTURE_SOURCE_PATH}" "${REMOTE_SSH_TARGET}:${REMOTE_TMP_DIR}/jumi-handoff-smoke.json"
scp_remote "${REMOTE_HELPER_PATH}" "${REMOTE_SSH_TARGET}:${REMOTE_TMP_DIR}/vm-lab-jumi-smoke-remote.sh"
scp_remote -r "${JUMI_SMOKE_TOOL_DIR}" "${REMOTE_SSH_TARGET}:${REMOTE_TMP_DIR}/jumi-smoke-src"

ssh_remote "bash -lc \"rm -rf '${REMOTE_JUMI_REPO_ROOT}/tools/jumi-smoke' && mkdir -p '${REMOTE_JUMI_REPO_ROOT}/tools' && cp -R '${REMOTE_TMP_DIR}/jumi-smoke-src' '${REMOTE_JUMI_REPO_ROOT}/tools/jumi-smoke' && cd '${REMOTE_JUMI_REPO_ROOT}/tools/jumi-smoke' && go build -o '${REMOTE_TMP_DIR}/jumi-smoke' .\""

collect_metric_values "jumi" "jumi" "metrics-jumi-start-${RUN_STAMP,,}" >"${start_jumi}"
collect_metric_values "${ARTIFACT_HANDOFF_SERVICE}" "ah" "metrics-ah-start-${RUN_STAMP,,}" >"${start_ah}"

if ssh_remote "
  export KUBECONFIG='${REMOTE_KUBECONFIG}'
  env \
    KUBECTL_CMD='kubectl' \
    JUMI_SPEC_PATH='${REMOTE_TMP_DIR}/jumi-handoff-smoke.json' \
    JUMI_SMOKE_BIN='${REMOTE_TMP_DIR}/jumi-smoke' \
    JUMI_GRPC_ADDR='127.0.0.1:${PORT_FORWARD_PORT}' \
    JUMI_NAMESPACE='${VM_NAMESPACE}' \
    JUMI_SERVICE='${JUMI_SERVICE}' \
    PORT_FORWARD_PORT='${PORT_FORWARD_PORT}' \
    JUMI_RUN_ID='${RUN_ID}' \
    JUMI_SAMPLE_RUN_ID='${SAMPLE_RUN_ID}' \
    bash '${REMOTE_TMP_DIR}/vm-lab-jumi-smoke-remote.sh'
"; then
  log_stage "remote smoke" "PASS" "runId=${RUN_ID}"
else
  log_stage "remote smoke" "FAIL" "runId=${RUN_ID}"
  exit 1
fi

collect_metric_values "jumi" "jumi" "metrics-jumi-end-${RUN_STAMP,,}" >"${end_jumi}"
collect_metric_values "${ARTIFACT_HANDOFF_SERVICE}" "ah" "metrics-ah-end-${RUN_STAMP,,}" >"${end_ah}"
collect_service_json "${ARTIFACT_HANDOFF_SERVICE}" "/v1/sampleRuns:lifecycle?sampleRunId=${SAMPLE_RUN_ID}" "lifecycle-ah-end-${RUN_STAMP,,}" >"${lifecycle_json}"
collect_service_json "${ARTIFACT_HANDOFF_SERVICE}" "/v1/artifacts:list?sampleRunId=${SAMPLE_RUN_ID}" "artifacts-ah-end-${RUN_STAMP,,}" >"${artifacts_json}"

finished_at="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

python3 - <<PY
import json
from pathlib import Path

fixture_path = Path("${FIXTURE_PATH}")
started_at = "${started_at}"
finished_at = "${finished_at}"
run_id = "${RUN_ID}"
sample_run_id = "${SAMPLE_RUN_ID}"
start_jumi_path = Path("${start_jumi}")
start_ah_path = Path("${start_ah}")
end_jumi_path = Path("${end_jumi}")
end_ah_path = Path("${end_ah}")
lifecycle_path = Path("${lifecycle_json}")
artifacts_path = Path("${artifacts_json}")
jumi_keys = [
    "jumi_jobs_created_total",
    "jumi_artifacts_registered_total",
    "jumi_input_resolve_requests_total",
    "jumi_input_remote_fetch_total",
    "jumi_input_materializations_total",
    "jumi_sample_runs_finalized_total",
    "jumi_gc_evaluate_requests_total",
]
ah_keys = [
    "ah_artifacts_registered_total",
    "ah_resolve_requests_total",
    "ah_fallback_total",
    "ah_gc_backlog_bytes",
]

def parse_metrics(path):
    values = {}
    for raw in path.read_text(encoding="utf-8").splitlines():
        line = raw.strip()
        if not line or line.startswith("#"):
            continue
        parts = line.split()
        if len(parts) != 2:
            continue
        key, value = parts
        if "{" in key:
            key = key.split("{", 1)[0]
        try:
            values[key] = float(value)
        except ValueError:
            continue
    return values

start_values = parse_metrics(start_jumi_path)
start_values.update(parse_metrics(start_ah_path))
end_values = parse_metrics(end_jumi_path)
end_values.update(parse_metrics(end_ah_path))

start_metrics = {k: start_values.get(k, 0.0) for k in jumi_keys + ah_keys}
end_metrics = {k: end_values.get(k, 0.0) for k in jumi_keys + ah_keys}

def load_last_json_payload(path):
    raw = path.read_text(encoding="utf-8")
    start = raw.find("{")
    end = raw.rfind("}")
    if start == -1 or end == -1 or end < start:
        raise ValueError(f"no json object found in {path}")
    return json.loads(raw[start:end + 1])

lifecycle = load_last_json_payload(lifecycle_path)
artifact_payload = load_last_json_payload(artifacts_path)

fixture = {
    "runId": run_id,
    "startedAt": started_at,
    "finishedAt": finished_at,
    "method": "OutsideSnapshot",
    "tags": {
        "env": "infra-lab",
        "profile": "jumi-ah-dev-live-smoke",
        "source": "JUMI",
        "collection": "live",
        "namespace": "${VM_NAMESPACE}",
    },
    "evidencePaths": {
        "smoke_fixture": "JUMI/deploy/devspace/fixtures/jumi-handoff-smoke.json",
        "smoke_result_doc": "JUMI/scripts/run-jumi-ah-dev-live-smoke-eval.sh",
        "live_run_id": run_id,
        "live_sample_run_id": sample_run_id,
    },
    "startMetrics": start_metrics,
    "endMetrics": end_metrics,
    "ahLifecycle": lifecycle,
    "ahArtifacts": artifact_payload.get("artifacts", []),
}
fixture_path.write_text(json.dumps(fixture, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
PY

if env FIXTURE_PATH="${FIXTURE_PATH}" OUTPUT_PATH="${SUMMARY_PATH}" PROFILE="smoke" \
  bash "${ROOT_DIR}/scripts/generate-kubeslint-jumi-ah-summary.sh"; then
  log_stage "summary generation" "PASS" "${SUMMARY_PATH}"
else
  log_stage "summary generation" "FAIL" "${SUMMARY_PATH}"
  exit 1
fi

resolved_slint_gate_bin="${SLINT_GATE_BIN}"
slint_gate_available="true"
if ! command -v "${resolved_slint_gate_bin}" >/dev/null 2>&1; then
  if [[ "${ALLOW_LOCAL_CHECKOUT_FALLBACK}" == "true" && "${resolved_slint_gate_bin}" == "slint-gate" && -x "${ROOT_DIR}/../kube-slint/slint-gate" ]]; then
    resolved_slint_gate_bin="${ROOT_DIR}/../kube-slint/slint-gate"
  else
    slint_gate_available="false"
  fi
fi

if [[ "${SLINT_GATE_MODE}" == "skip" ]]; then
  log_stage "slint gate" "SKIPPED" "mode=skip summary=${SUMMARY_PATH}"
  printf 'smoke artifacts: fixture=%s summary=%s gate=%s\n' "${FIXTURE_PATH}" "${SUMMARY_PATH}" "${GATE_PATH}"
  exit 0
fi

if [[ "${slint_gate_available}" != "true" ]]; then
  if [[ "${SLINT_GATE_MODE}" == "optional" ]]; then
    log_stage "slint gate" "SKIPPED" "missing ${SLINT_GATE_BIN}"
    printf 'smoke artifacts: fixture=%s summary=%s gate=%s\n' "${FIXTURE_PATH}" "${SUMMARY_PATH}" "${GATE_PATH}"
    exit 0
  fi
  log_stage "slint gate" "FAIL" "missing ${SLINT_GATE_BIN}"
  exit 1
fi

if "${resolved_slint_gate_bin}" \
  --measurement-summary "${SUMMARY_PATH}" \
  --policy "${POLICY_FILE}" \
  --output "${GATE_PATH}"; then
  log_stage "slint gate" "PASS" "${GATE_PATH}"
else
  log_stage "slint gate" "FAIL" "${GATE_PATH}"
  exit 1
fi

if [[ "${PUBLISH_SHIFT_LEFT_OBSERVABILITY}" == "true" ]]; then
  [[ -n "${PUBLISH_SHIFT_LEFT_OBSERVABILITY_SCRIPT}" ]] || {
    echo "missing publish script path: PUBLISH_SHIFT_LEFT_OBSERVABILITY_SCRIPT" >&2
    exit 1
  }
  SLI_SUMMARY_PATH="${SUMMARY_PATH}" GATE_SUMMARY_PATH="${GATE_PATH}" POLICY_FILE="${POLICY_FILE}" \
    bash "${PUBLISH_SHIFT_LEFT_OBSERVABILITY_SCRIPT}"
fi

python3 - <<PY
import json
from pathlib import Path

fixture = json.loads(Path("${FIXTURE_PATH}").read_text(encoding="utf-8"))
summary = json.loads(Path("${SUMMARY_PATH}").read_text(encoding="utf-8"))
gate = json.loads(Path("${GATE_PATH}").read_text(encoding="utf-8"))

print(f"fixture: ${FIXTURE_PATH}")
print(f"summary: ${SUMMARY_PATH}")
print(f"gate: ${GATE_PATH}")
print(f"live_run_id={fixture['runId']}")
print(f"live_sample_run_id={fixture['evidencePaths']['live_sample_run_id']}")
print(f"results={len(summary.get('results', []))} gate_result={gate.get('gate_result')}")
print(f"evaluation_status={gate.get('evaluation_status')} measurement_status={gate.get('measurement_status')}")
print(f"overall_message={gate.get('overall_message')}")
print(f"published_shift_left_observability={'true' if '${PUBLISH_SHIFT_LEFT_OBSERVABILITY}' == 'true' else 'false'}")
PY
