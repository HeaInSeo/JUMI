#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
REMOTE_SSH_TARGET="${REMOTE_SSH_TARGET:-seoy@100.123.80.48}"
REMOTE_KUBECONFIG="${REMOTE_KUBECONFIG:-/opt/go/src/github.com/HeaInSeo/infra-lab/kubeconfig}"
REMOTE_JUMI_REPO_ROOT="${REMOTE_JUMI_REPO_ROOT:-/tmp/jumi-runtime-refresh}"
REMOTE_GIT_REF="${REMOTE_GIT_REF:-$(git -C "${ROOT_DIR}" rev-parse --abbrev-ref HEAD)}"
REMOTE_AH_REPO_ROOT="${REMOTE_AH_REPO_ROOT:-/tmp/artifact-handoff-refresh}"
REMOTE_AH_GIT_REF="${REMOTE_AH_GIT_REF:-main}"
REMOTE_TMP_DIR="${REMOTE_TMP_DIR:-/tmp/jumi-same-node-local-reuse}"
VM_NAMESPACE="${VM_NAMESPACE:-jumi-ah-dev}"
FIXTURE_TEMPLATE="${FIXTURE_TEMPLATE:-${ROOT_DIR}/deploy/devspace/fixtures/jumi-same-node-local-reuse-smoke.json}"
RUNTIME_SHORTCUT_IMAGE_REPO="${RUNTIME_SHORTCUT_IMAGE_REPO:-harbor.10.113.24.96.nip.io/batch-int/jumi}"
RUNTIME_SHORTCUT_IMAGE_TAG="${RUNTIME_SHORTCUT_IMAGE_TAG:-runtime-shortcut-local-reuse-$(git -C "${ROOT_DIR}" rev-parse --short HEAD)}"
RUNTIME_SHORTCUT_IMAGE="${RUNTIME_SHORTCUT_IMAGE:-${RUNTIME_SHORTCUT_IMAGE_REPO}:${RUNTIME_SHORTCUT_IMAGE_TAG}}"
AH_IMAGE_REPO="${AH_IMAGE_REPO:-harbor.10.113.24.96.nip.io/batch-int/artifact-handoff}"
AH_IMAGE_TAG="${AH_IMAGE_TAG:-node-local-handoff-$(date -u +%Y%m%d%H%M%S)}"
AH_IMAGE="${AH_IMAGE:-${AH_IMAGE_REPO}:${AH_IMAGE_TAG}}"
EVAL_SCRIPT="${EVAL_SCRIPT:-${ROOT_DIR}/scripts/run-jumi-ah-dev-live-smoke-eval.sh}"
PUBLISH_JUMI_SERVICE_SCRIPT="${PUBLISH_JUMI_SERVICE_SCRIPT:-${ROOT_DIR}/scripts/publish-jumi-service-ko-remote.sh}"
FIXTURE_PATH="$(mktemp "${ROOT_DIR}/artifacts/devspace/jumi-same-node-local-reuse-fixture.XXXXXX.json")"

ssh_remote() {
  ssh -F /dev/null -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null "$REMOTE_SSH_TARGET" "$@"
}

cleanup() {
  rm -f "${FIXTURE_PATH}"
  if [[ -n "${ORIGINAL_AH_GRPC_TARGET:-}" ]]; then
    ssh_remote "set -euo pipefail; export KUBECONFIG='${REMOTE_KUBECONFIG}'; kubectl -n '${VM_NAMESPACE}' set env deploy/jumi JUMI_AH_GRPC_TARGET='${ORIGINAL_AH_GRPC_TARGET}' >/dev/null; kubectl -n '${VM_NAMESPACE}' rollout status deploy/jumi --timeout=180s >/dev/null"
  fi
}
trap cleanup EXIT

mkdir -p "${ROOT_DIR}/artifacts/devspace"

ORIGINAL_AH_GRPC_TARGET="$(ssh_remote "set -euo pipefail; export KUBECONFIG='${REMOTE_KUBECONFIG}'; kubectl -n '${VM_NAMESPACE}' get deploy jumi -o jsonpath='{range .spec.template.spec.containers[0].env[?(@.name==\"JUMI_AH_GRPC_TARGET\")]}{.value}{end}'")"

WORKER_NODE_NAME="$(ssh_remote "set -euo pipefail; export KUBECONFIG='${REMOTE_KUBECONFIG}'; node=\$(kubectl get nodes -l '!node-role.kubernetes.io/control-plane' --no-headers 2>/dev/null | awk '\$2==\"Ready\" {print \$1; exit}'); if [ -z \"\${node}\" ]; then node=\$(kubectl get nodes --no-headers | awk '\$2==\"Ready\" {print \$1; exit}'); fi; if [ -z \"\${node}\" ]; then echo 'no Ready node found' >&2; exit 1; fi; printf '%s' \"\${node}\"")"

python3 - <<PY
from pathlib import Path
fixture = Path("${FIXTURE_TEMPLATE}").read_text(encoding="utf-8")
fixture = fixture.replace("__RUNTIME_SHORTCUT_IMAGE__", "${RUNTIME_SHORTCUT_IMAGE}")
fixture = fixture.replace("__WORKER_NODE_NAME__", "${WORKER_NODE_NAME}")
Path("${FIXTURE_PATH}").write_text(fixture, encoding="utf-8")
PY

ssh_remote "
  set -euo pipefail
  git -C '${REMOTE_JUMI_REPO_ROOT}' fetch origin
  git -C '${REMOTE_JUMI_REPO_ROOT}' checkout '${REMOTE_GIT_REF}'
  git -C '${REMOTE_JUMI_REPO_ROOT}' reset --hard 'origin/${REMOTE_GIT_REF}'
  if [ ! -d '${REMOTE_AH_REPO_ROOT}/.git' ]; then
    git clone https://github.com/HeaInSeo/artifact-handoff.git '${REMOTE_AH_REPO_ROOT}'
  fi
  git -C '${REMOTE_AH_REPO_ROOT}' fetch origin
  git -C '${REMOTE_AH_REPO_ROOT}' checkout '${REMOTE_AH_GIT_REF}'
  git -C '${REMOTE_AH_REPO_ROOT}' reset --hard 'origin/${REMOTE_AH_GIT_REF}'
"

REMOTE_SSH_TARGET="${REMOTE_SSH_TARGET}" \
REMOTE_JUMI_REPO_ROOT="${REMOTE_JUMI_REPO_ROOT}" \
REMOTE_KUBECONFIG="${REMOTE_KUBECONFIG}" \
"${PUBLISH_JUMI_SERVICE_SCRIPT}"

ssh_remote "
  set -euo pipefail
  cd '${REMOTE_AH_REPO_ROOT}'
  podman build -f Containerfile -t '${AH_IMAGE}' .
  podman push '${AH_IMAGE}'
"

ssh_remote "
  set -euo pipefail
  export KUBECONFIG='${REMOTE_KUBECONFIG}'
  kubectl -n '${VM_NAMESPACE}' set image deployment/artifact-handoff artifact-handoff='${AH_IMAGE}'
  kubectl -n '${VM_NAMESPACE}' rollout status deployment/artifact-handoff --timeout=180s
"

ssh_remote "
  set -euo pipefail
  export KUBECONFIG='${REMOTE_KUBECONFIG}'
  kubectl -n '${VM_NAMESPACE}' set env deploy/jumi JUMI_AH_GRPC_TARGET- >/dev/null
  kubectl -n '${VM_NAMESPACE}' rollout status deploy/jumi --timeout=180s
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
