#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
CONTAINERFILE_PATH="${CONTAINERFILE_PATH:-${ROOT_DIR}/Containerfile}"
RUNTIME_REPO_ROOT="${RUNTIME_REPO_ROOT:-/opt/go/src/github.com/HeaInSeo/node-artifact-runtime}"
OUTPUT_PATH="${OUTPUT_PATH:-${ROOT_DIR}/artifacts/devspace/runtime/runtime-alignment.json}"

need_cmd() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "missing command: $1" >&2
    exit 1
  }
}

need_cmd git
need_cmd python3

[[ -f "${CONTAINERFILE_PATH}" ]] || {
  echo "missing Containerfile: ${CONTAINERFILE_PATH}" >&2
  exit 1
}

mkdir -p "$(dirname "${OUTPUT_PATH}")"

PINNED_REF="$(
  python3 - <<PY
import re
from pathlib import Path
text = Path("${CONTAINERFILE_PATH}").read_text(encoding="utf-8")
match = re.search(r'node-artifact-runtime/cmd/node-artifact-runtime@([A-Za-z0-9._-]+)', text)
if not match:
    raise SystemExit("runtime pin not found in Containerfile")
print(match.group(1))
PY
)"

HEAD_REF=""
HEAD_DESCRIBE=""
if [[ -d "${RUNTIME_REPO_ROOT}/.git" ]]; then
  HEAD_REF="$(git -C "${RUNTIME_REPO_ROOT}" rev-parse HEAD)"
  HEAD_DESCRIBE="$(git -C "${RUNTIME_REPO_ROOT}" describe --tags --always)"
fi

python3 - <<PY
import json
from pathlib import Path

pinned = "${PINNED_REF}"
head = "${HEAD_REF}"
head_describe = "${HEAD_DESCRIBE}"

payload = {
    "containerfile": "${CONTAINERFILE_PATH}",
    "runtimeRepoRoot": "${RUNTIME_REPO_ROOT}",
    "pinnedRef": pinned,
    "runtimeRepoHead": head,
    "runtimeRepoDescribe": head_describe,
    "aligned": bool(head) and pinned == head,
}
Path("${OUTPUT_PATH}").write_text(json.dumps(payload, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
print(json.dumps(payload, ensure_ascii=False, indent=2))
PY
