#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
REMOTE_SSH_TARGET="${REMOTE_SSH_TARGET:-seoy@100.123.80.48}"
REMOTE_KUBECONFIG="${REMOTE_KUBECONFIG:-/opt/go/src/github.com/HeaInSeo/infra-lab/kubeconfig}"
REMOTE_JUMI_REPO_ROOT="${REMOTE_JUMI_REPO_ROOT:-/tmp/jumi-runtime-refresh}"
REMOTE_AH_REPO_ROOT="${REMOTE_AH_REPO_ROOT:-/tmp/artifact-handoff-refresh}"
REMOTE_AH_GIT_REF="${REMOTE_AH_GIT_REF:-main}"
VM_NAMESPACE="${VM_NAMESPACE:-jumi-ah-dev}"
ARTIFACT_SOURCE_MANIFEST="${ARTIFACT_SOURCE_MANIFEST:-${ROOT_DIR}/deploy/devspace/jumi-simple-http-artifact-source.yaml}"
FIXTURE_TEMPLATE="${FIXTURE_TEMPLATE:-${ROOT_DIR}/deploy/devspace/fixtures/jumi-remote-fetch-simple-http-smoke.json}"
RUNTIME_SHORTCUT_IMAGE_REPO="${RUNTIME_SHORTCUT_IMAGE_REPO:-harbor.10.113.24.96.nip.io/batch-int/jumi}"
RUNTIME_SHORTCUT_IMAGE_TAG="${RUNTIME_SHORTCUT_IMAGE_TAG:-runtime-shortcut-simple-http-$(git -C "${ROOT_DIR}" rev-parse --short HEAD)}"
RUNTIME_SHORTCUT_IMAGE="${RUNTIME_SHORTCUT_IMAGE:-${RUNTIME_SHORTCUT_IMAGE_REPO}:${RUNTIME_SHORTCUT_IMAGE_TAG}}"
ENABLE_HTTP_AH="${ENABLE_HTTP_AH:-0}"
AH_IMAGE_REPO="${AH_IMAGE_REPO:-harbor.10.113.24.96.nip.io/batch-int/artifact-handoff}"
AH_IMAGE_TAG="${AH_IMAGE_TAG:-remote-fetch-$(date -u +%Y%m%d%H%M%S)}"
AH_IMAGE="${AH_IMAGE:-${AH_IMAGE_REPO}:${AH_IMAGE_TAG}}"
EVAL_SCRIPT="${EVAL_SCRIPT:-${ROOT_DIR}/scripts/run-jumi-ah-dev-live-smoke-eval.sh}"
FIXTURE_PATH="$(mktemp "${ROOT_DIR}/artifacts/devspace/jumi-remote-fetch-simple-http-fixture.XXXXXX.json")"
ARTIFACT_SOURCE_REMOTE_PATH="${REMOTE_TMP_DIR:-/tmp/jumi-simple-http-artifact-source}/simple-http-artifact-source.yaml"
ORIGINAL_AH_GRPC_TARGET=""

ssh_remote() {
  ssh -F /dev/null -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null "$REMOTE_SSH_TARGET" "$@"
}

scp_remote() {
  scp -F /dev/null -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null "$@"
}

cleanup() {
  rm -f "${FIXTURE_PATH}"
  if [[ "${ENABLE_HTTP_AH}" == "1" && -n "${ORIGINAL_AH_GRPC_TARGET:-}" ]]; then
    ssh_remote "set -euo pipefail; export KUBECONFIG='${REMOTE_KUBECONFIG}'; kubectl -n '${VM_NAMESPACE}' set env deploy/jumi JUMI_AH_GRPC_TARGET='${ORIGINAL_AH_GRPC_TARGET}' >/dev/null; kubectl -n '${VM_NAMESPACE}' rollout status deploy/jumi --timeout=180s >/dev/null" >/dev/null 2>&1 || true
  fi
  ssh_remote "export KUBECONFIG='${REMOTE_KUBECONFIG}'; kubectl -n '${VM_NAMESPACE}' delete -f '${ARTIFACT_SOURCE_REMOTE_PATH}' --ignore-not-found; rm -rf '$(dirname "${ARTIFACT_SOURCE_REMOTE_PATH}")'" >/dev/null 2>&1 || true
}
trap cleanup EXIT

mkdir -p "${ROOT_DIR}/artifacts/devspace"

if [[ "${ENABLE_HTTP_AH}" == "1" ]]; then
  ORIGINAL_AH_GRPC_TARGET="$(ssh_remote "set -euo pipefail; export KUBECONFIG='${REMOTE_KUBECONFIG}'; kubectl -n '${VM_NAMESPACE}' get deploy jumi -o jsonpath='{range .spec.template.spec.containers[0].env[?(@.name==\"JUMI_AH_GRPC_TARGET\")]}{.value}{end}'")"
  ssh_remote "
    set -euo pipefail
    if [ ! -d '${REMOTE_AH_REPO_ROOT}/.git' ]; then
      git clone https://github.com/HeaInSeo/artifact-handoff.git '${REMOTE_AH_REPO_ROOT}'
    fi
    git -C '${REMOTE_AH_REPO_ROOT}' fetch origin
    git -C '${REMOTE_AH_REPO_ROOT}' checkout '${REMOTE_AH_GIT_REF}'
    git -C '${REMOTE_AH_REPO_ROOT}' reset --hard 'origin/${REMOTE_AH_GIT_REF}'
    cd '${REMOTE_AH_REPO_ROOT}'
    podman build -f Containerfile -t '${AH_IMAGE}' .
    podman push '${AH_IMAGE}'
    export KUBECONFIG='${REMOTE_KUBECONFIG}'
    kubectl -n '${VM_NAMESPACE}' set image deployment/artifact-handoff artifact-handoff='${AH_IMAGE}'
    kubectl -n '${VM_NAMESPACE}' rollout status deployment/artifact-handoff --timeout=180s
    kubectl -n '${VM_NAMESPACE}' set env deploy/jumi JUMI_AH_GRPC_TARGET- >/dev/null
    kubectl -n '${VM_NAMESPACE}' rollout status deploy/jumi --timeout=180s
  "
fi

python3 - <<PY
from pathlib import Path
fixture = Path("${FIXTURE_TEMPLATE}").read_text(encoding="utf-8")
fixture = fixture.replace("__RUNTIME_SHORTCUT_IMAGE__", "${RUNTIME_SHORTCUT_IMAGE}")
Path("${FIXTURE_PATH}").write_text(fixture, encoding="utf-8")
PY

ssh_remote "mkdir -p '$(dirname "${ARTIFACT_SOURCE_REMOTE_PATH}")'"
scp_remote "${ARTIFACT_SOURCE_MANIFEST}" "${REMOTE_SSH_TARGET}:${ARTIFACT_SOURCE_REMOTE_PATH}"

ssh_remote "
  set -euo pipefail
  export KUBECONFIG='${REMOTE_KUBECONFIG}'
  kubectl -n '${VM_NAMESPACE}' apply -f '${ARTIFACT_SOURCE_REMOTE_PATH}'
  kubectl -n '${VM_NAMESPACE}' rollout status deployment/simple-http-artifact-source --timeout=180s
  kubectl -n '${VM_NAMESPACE}' get svc simple-http-artifact-source
"

ssh_remote "
  set -euo pipefail
  cd '${REMOTE_JUMI_REPO_ROOT}'
  podman build -f Containerfile -t '${RUNTIME_SHORTCUT_IMAGE}' .
  podman push '${RUNTIME_SHORTCUT_IMAGE}'
"

FIXTURE_SOURCE_PATH="${FIXTURE_PATH}" \
REMOTE_SSH_TARGET="${REMOTE_SSH_TARGET}" \
REMOTE_KUBECONFIG="${REMOTE_KUBECONFIG}" \
REMOTE_JUMI_REPO_ROOT="${REMOTE_JUMI_REPO_ROOT}" \
VM_NAMESPACE="${VM_NAMESPACE}" \
bash "${EVAL_SCRIPT}"
