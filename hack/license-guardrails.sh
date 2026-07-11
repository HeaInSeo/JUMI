#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

is_owner_scoped_exception() {
  case "$1" in
    github.com/HeaInSeo/JUMI|github.com/HeaInSeo/spawner|github.com/HeaInSeo/utils)
      return 0
      ;;
    *)
      return 1
      ;;
  esac
}

license_file_for_dir() {
  local dir="$1"
  find "$dir" -maxdepth 1 -type f \( \
    -iname 'LICENSE*' -o \
    -iname 'COPYING*' -o \
    -iname 'NOTICE*' \
  \) | sort | head -n 1
}

classify_license() {
  local file="$1"
  if grep -Eiq 'GNU (Affero )?General Public License|GNU Lesser General Public License|Mozilla Public License|Eclipse Public License|COMMON DEVELOPMENT AND DISTRIBUTION LICENSE|Creative Commons' "$file"; then
    echo "forbidden"
    return
  fi
  if grep -Eiq 'Apache License' "$file" && grep -Eiq 'Version 2\.0|Apache-2\.0' "$file"; then
    echo "Apache-2.0"
    return
  fi
  if grep -Eiq 'MIT License|Permission is hereby granted, free of charge' "$file"; then
    echo "MIT"
    return
  fi
  if grep -Eiq 'Redistribution and use in source and binary forms' "$file"; then
    echo "BSD"
    return
  fi
  if grep -Eiq 'ISC License|Permission to use, copy, modify, and/or distribute this software' "$file"; then
    echo "ISC"
    return
  fi
  echo "unknown"
}

failures=0
modules_file="$(mktemp)"
trap 'rm -f "$modules_file"' EXIT

if ! go list -deps -f '{{if .Module}}{{.Module.Path}}{{"\t"}}{{.Module.Dir}}{{end}}' ./cmd/jumi | sort -u >"$modules_file"; then
  echo "FAIL: go list -deps failed; cannot evaluate dependency licenses"
  exit 1
fi

while IFS=$'\t' read -r module dir; do
  if [[ -z "$module" ]]; then
    continue
  fi
  if is_owner_scoped_exception "$module"; then
    echo "ignore owner-scoped: $module"
    continue
  fi
  if [[ -z "$dir" || ! -d "$dir" ]]; then
    echo "FAIL: module directory unavailable: $module"
    failures=$((failures + 1))
    continue
  fi
  license_file="$(license_file_for_dir "$dir")"
  if [[ -z "$license_file" ]]; then
    echo "FAIL: missing license file: $module ($dir)"
    failures=$((failures + 1))
    continue
  fi
  license_type="$(classify_license "$license_file")"
  case "$license_type" in
    Apache-2.0|MIT|BSD|ISC)
      echo "ok: $module: $license_type"
      ;;
    forbidden|unknown)
      echo "FAIL: $module: $license_type license in $license_file"
      failures=$((failures + 1))
      ;;
    *)
      echo "FAIL: $module: unexpected license classifier result $license_type"
      failures=$((failures + 1))
      ;;
  esac
done <"$modules_file"

if [[ "$failures" -ne 0 ]]; then
  echo "license guardrail failed: $failures issue(s)"
  exit 1
fi

echo "license guardrail passed"
