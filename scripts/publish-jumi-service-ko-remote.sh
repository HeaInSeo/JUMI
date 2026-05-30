#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
REMOTE_SSH_TARGET="${REMOTE_SSH_TARGET:-seoy@100.123.80.48}"
REMOTE_JUMI_REPO_ROOT="${REMOTE_JUMI_REPO_ROOT:-/tmp/jumi-runtime-refresh}"
REMOTE_KUBECONFIG="${REMOTE_KUBECONFIG:-/opt/go/src/github.com/HeaInSeo/infra-lab/kubeconfig}"
REMOTE_GO_BIN="${REMOTE_GO_BIN:-/usr/local/go/bin/go}"
REMOTE_KO_BIN="${REMOTE_KO_BIN:-\$HOME/.local/bin/ko}"
REGISTRY_HOST="${REGISTRY_HOST:-harbor.10.113.24.96.nip.io}"
KO_DOCKER_REPO="${KO_DOCKER_REPO:-${REGISTRY_HOST}/batch-int}"
SYNC_BACKUP_REGISTRY="${SYNC_BACKUP_REGISTRY:-false}"
BACKUP_REGISTRY_HOST="${BACKUP_REGISTRY_HOST:-ghcr.io}"
BACKUP_KO_DOCKER_REPO="${BACKUP_KO_DOCKER_REPO:-ghcr.io/heainseo}"
KO_IMPORT_PATH="${KO_IMPORT_PATH:-./cmd/jumi}"
KO_FLAGS="${KO_FLAGS:--B}"
DEPLOY_NAMESPACE="${DEPLOY_NAMESPACE:-jumi-ah-dev}"
DEPLOY_NAME="${DEPLOY_NAME:-jumi}"
DEPLOY_CONTAINER="${DEPLOY_CONTAINER:-jumi}"
IMAGE_REF_FILE="${IMAGE_REF_FILE:-${ROOT_DIR}/artifacts/devspace/ko/jumi-service-image-ref.txt}"
BACKUP_IMAGE_REF_FILE="${BACKUP_IMAGE_REF_FILE:-${ROOT_DIR}/artifacts/devspace/ko/jumi-service-image-ref.backup.txt}"
SYNC_MANIFEST_FILE="${SYNC_MANIFEST_FILE:-${ROOT_DIR}/artifacts/devspace/ko/jumi-service-image-sync.json}"

ssh_remote() {
  ssh -F /dev/null -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null "$REMOTE_SSH_TARGET" "$@"
}

mkdir -p "$(dirname "${IMAGE_REF_FILE}")"
mkdir -p "$(dirname "${BACKUP_IMAGE_REF_FILE}")"
mkdir -p "$(dirname "${SYNC_MANIFEST_FILE}")"

REMOTE_SSH_TARGET="${REMOTE_SSH_TARGET}" \
REGISTRY_HOST="${REGISTRY_HOST}" \
SYNC_BACKUP_REGISTRY="${SYNC_BACKUP_REGISTRY}" \
BACKUP_REGISTRY_HOST="${BACKUP_REGISTRY_HOST}" \
REMOTE_GO_BIN="${REMOTE_GO_BIN}" \
REMOTE_KO_BIN="${REMOTE_KO_BIN}" \
"${ROOT_DIR}/scripts/preflight-ko-remote.sh"

PRIMARY_IMAGE_REF="$(
  ssh_remote "
    set -euo pipefail
    export PATH=\$HOME/.local/bin:/usr/local/go/bin:\$PATH
    cd '${REMOTE_JUMI_REPO_ROOT}'
    export KO_DOCKER_REPO='${KO_DOCKER_REPO}'
    ${REMOTE_KO_BIN} build ${KO_FLAGS} '${KO_IMPORT_PATH}' | tail -n 1
  "
)"

BACKUP_IMAGE_REF=""
if [[ "${SYNC_BACKUP_REGISTRY}" == "true" ]]; then
  BACKUP_IMAGE_REF="$(
    ssh_remote "
      set -euo pipefail
      export PATH=\$HOME/.local/bin:/usr/local/go/bin:\$PATH
      cd '${REMOTE_JUMI_REPO_ROOT}'
      export KO_DOCKER_REPO='${BACKUP_KO_DOCKER_REPO}'
      ${REMOTE_KO_BIN} build ${KO_FLAGS} '${KO_IMPORT_PATH}' | tail -n 1
    "
  )"
fi

printf '%s\n' "${PRIMARY_IMAGE_REF}" | tee "${IMAGE_REF_FILE}"
if [[ -n "${BACKUP_IMAGE_REF}" ]]; then
  printf '%s\n' "${BACKUP_IMAGE_REF}" | tee "${BACKUP_IMAGE_REF_FILE}"
else
  : >"${BACKUP_IMAGE_REF_FILE}"
fi

ssh_remote "
  set -euo pipefail
  export KUBECONFIG='${REMOTE_KUBECONFIG}'
  kubectl -n '${DEPLOY_NAMESPACE}' set image deployment/'${DEPLOY_NAME}' '${DEPLOY_CONTAINER}'='${PRIMARY_IMAGE_REF}'
  kubectl -n '${DEPLOY_NAMESPACE}' rollout status deployment/'${DEPLOY_NAME}' --timeout=180s
"

python3 - <<PY
import json
from pathlib import Path

manifest = {
    "sourceOfTruth": "github",
    "primaryRegistry": "${REGISTRY_HOST}",
    "backupRegistry": "${BACKUP_REGISTRY_HOST}" if "${SYNC_BACKUP_REGISTRY}" == "true" else "",
    "syncEnabled": "${SYNC_BACKUP_REGISTRY}" == "true",
    "koImportPath": "${KO_IMPORT_PATH}",
    "primaryImageRef": "${PRIMARY_IMAGE_REF}",
    "backupImageRef": "${BACKUP_IMAGE_REF}",
}
Path("${SYNC_MANIFEST_FILE}").write_text(json.dumps(manifest, ensure_ascii=False, indent=2) + "\\n", encoding="utf-8")
PY

if [[ -n "${BACKUP_IMAGE_REF}" ]]; then
  echo "[ko-publish] deployed primary ${PRIMARY_IMAGE_REF}"
  echo "[ko-publish] synced backup ${BACKUP_IMAGE_REF}"
else
  echo "[ko-publish] deployed ${PRIMARY_IMAGE_REF}"
fi
