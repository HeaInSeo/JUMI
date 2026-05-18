#!/usr/bin/env bash
set -euo pipefail

REGISTRY_HOST="${REGISTRY_HOST:-harbor.10.113.24.96.nip.io}"
REGISTRY_URL="${REGISTRY_URL:-https://${REGISTRY_HOST}/v2/}"
REMOTE_SSH_TARGET="${REMOTE_SSH_TARGET:-seoy@100.123.80.48}"
MODE="local"

if [[ "${1:-}" == "--remote" ]]; then
  MODE="remote"
fi

need_cmd() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "[preflight] missing command: $1" >&2
    exit 1
  }
}

check_registry_http() {
  local url="$1"
  local code
  code="$(curl -k -sS -o /dev/null -w '%{http_code}' --connect-timeout 5 "$url" || true)"
  case "$code" in
    200|401|403)
      echo "[preflight] registry reachable: ${url} (http ${code})"
      ;;
    *)
      echo "[preflight] registry is not reachable from this host: ${url}" >&2
      if [[ -n "$code" ]]; then
        echo "[preflight] http code: ${code}" >&2
      fi
      exit 1
      ;;
  esac
}

need_cmd curl

if [[ "$MODE" == "remote" ]]; then
  need_cmd ssh
  echo "[preflight] checking remote publish authority host: ${REMOTE_SSH_TARGET}"
  ssh -F /dev/null -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null \
    "$REMOTE_SSH_TARGET" \
    "REGISTRY_URL='${REGISTRY_URL}' bash -lc 'code=\$(curl -k -sS -o /dev/null -w \"%{http_code}\" --connect-timeout 5 \"\$REGISTRY_URL\" || true); case \"\$code\" in 200|401|403) echo \"[preflight] registry reachable: \$REGISTRY_URL (http \$code)\" ;; *) echo \"[preflight] registry is not reachable from remote host: \$REGISTRY_URL\" >&2; if [ -n \"\$code\" ]; then echo \"[preflight] http code: \$code\" >&2; fi; exit 1 ;; esac'" 
  exit 0
fi

echo "[preflight] checking local publish reachability: ${REGISTRY_URL}"
check_registry_http "${REGISTRY_URL}"
