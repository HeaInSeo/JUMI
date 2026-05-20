#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
REMOTE_SSH_TARGET="${REMOTE_SSH_TARGET:-seoy@100.123.80.48}"
REMOTE_JUMI_REPO_ROOT="${REMOTE_JUMI_REPO_ROOT:-/tmp/jumi-runtime-refresh}"
REMOTE_KUBECONFIG="${REMOTE_KUBECONFIG:-/opt/go/src/github.com/HeaInSeo/infra-lab/kubeconfig}"
REMOTE_GO_BIN="${REMOTE_GO_BIN:-/usr/local/go/bin/go}"
REMOTE_KO_BIN="${REMOTE_KO_BIN:-$HOME/.local/bin/ko}"
REGISTRY_HOST="${REGISTRY_HOST:-harbor.10.113.24.96.nip.io}"
KO_DOCKER_REPO="${KO_DOCKER_REPO:-${REGISTRY_HOST}/batch-int}"
KO_IMPORT_PATH="${KO_IMPORT_PATH:-./cmd/jumi}"
KO_FLAGS="${KO_FLAGS:--B}"
DEPLOY_NAMESPACE="${DEPLOY_NAMESPACE:-jumi-ah-dev}"
DEPLOY_NAME="${DEPLOY_NAME:-jumi}"
DEPLOY_CONTAINER="${DEPLOY_CONTAINER:-jumi}"
IMAGE_REF_FILE="${IMAGE_REF_FILE:-${ROOT_DIR}/artifacts/devspace/ko/jumi-service-image-ref.txt}"

ssh_remote() {
  ssh -F /dev/null -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null "$REMOTE_SSH_TARGET" "$@"
}

mkdir -p "$(dirname "${IMAGE_REF_FILE}")"

REMOTE_SSH_TARGET="${REMOTE_SSH_TARGET}" \
REGISTRY_HOST="${REGISTRY_HOST}" \
REMOTE_GO_BIN="${REMOTE_GO_BIN}" \
REMOTE_KO_BIN="${REMOTE_KO_BIN}" \
"${ROOT_DIR}/scripts/preflight-ko-remote.sh"

IMAGE_REF="$(
  ssh_remote "
    set -euo pipefail
    export PATH=\$HOME/.local/bin:/usr/local/go/bin:\$PATH
    cd '${REMOTE_JUMI_REPO_ROOT}'
    export KO_DOCKER_REPO='${KO_DOCKER_REPO}'
    ${REMOTE_KO_BIN} build ${KO_FLAGS} '${KO_IMPORT_PATH}' | tail -n 1
  "
)"

printf '%s\n' "${IMAGE_REF}" | tee "${IMAGE_REF_FILE}"

ssh_remote "
  set -euo pipefail
  export KUBECONFIG='${REMOTE_KUBECONFIG}'
  kubectl -n '${DEPLOY_NAMESPACE}' set image deployment/'${DEPLOY_NAME}' '${DEPLOY_CONTAINER}'='${IMAGE_REF}'
  kubectl -n '${DEPLOY_NAMESPACE}' rollout status deployment/'${DEPLOY_NAME}' --timeout=180s
"

echo "[ko-publish] deployed ${IMAGE_REF}"
