#!/usr/bin/env bash
set -euo pipefail

REMOTE_SSH_TARGET="${REMOTE_SSH_TARGET:-seoy@100.123.80.48}"
REGISTRY_HOST="${REGISTRY_HOST:-harbor.10.113.24.96.nip.io}"
SYNC_BACKUP_REGISTRY="${SYNC_BACKUP_REGISTRY:-false}"
BACKUP_REGISTRY_HOST="${BACKUP_REGISTRY_HOST:-ghcr.io}"
REMOTE_GO_BIN="${REMOTE_GO_BIN:-/usr/local/go/bin/go}"
REMOTE_KO_BIN="${REMOTE_KO_BIN:-\$HOME/.local/bin/ko}"

ssh_remote() {
  ssh -F /dev/null -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null "$REMOTE_SSH_TARGET" "$@"
}

echo "[ko-preflight] target=${REMOTE_SSH_TARGET}"
echo "[ko-preflight] registry=${REGISTRY_HOST}"
if [[ "${SYNC_BACKUP_REGISTRY}" == "true" ]]; then
  echo "[ko-preflight] backup-registry=${BACKUP_REGISTRY_HOST}"
fi

ssh_remote "
  set -euo pipefail
  test -x '${REMOTE_GO_BIN}'
  test -x ${REMOTE_KO_BIN}
  test -f \"\$HOME/.docker/config.json\"
  python3 - <<'PY'
import json
import pathlib
import socket
import ssl

registries = ['${REGISTRY_HOST}']
if '${SYNC_BACKUP_REGISTRY}' == 'true':
    registries.append('${BACKUP_REGISTRY_HOST}')
cfg = pathlib.Path.home() / '.docker' / 'config.json'
data = json.loads(cfg.read_text())
for registry in registries:
    if registry not in data.get('auths', {}):
        raise SystemExit('missing docker auth entry for %s' % registry)
    ctx = ssl.create_default_context()
    with ctx.wrap_socket(socket.socket(), server_hostname=registry) as s:
        s.settimeout(5)
        s.connect((registry, 443))
        print('tls-ok %s' % registry)
PY
"

echo "[ko-preflight] ok"
