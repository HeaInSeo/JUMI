#!/usr/bin/env bash
set -euo pipefail

failures=0

fail() {
  echo "::error::$*"
  failures=$((failures + 1))
}

pass() {
  echo "ok: $*"
}

require_file() {
  local path="$1"
  if [[ -f "$path" ]]; then
    pass "found $path"
  else
    fail "missing required file: $path"
  fi
}

require_grep() {
  local pattern="$1"
  local path="$2"
  local label="$3"
  if grep -Eq -- "$pattern" "$path"; then
    pass "$label"
  else
    fail "$label"
  fi
}

reject_grep() {
  local pattern="$1"
  local path="$2"
  local label="$3"
  if grep -Eq -- "$pattern" "$path"; then
    fail "$label"
  else
    pass "$label"
  fi
}

check_source_of_truth() {
  echo "== source-of-truth guardrails =="
  require_file docs/JUMI_K8S_JOB_LABEL_CONTRACT.md
  require_file docs/JUMI_DESIGN.ko.md
  require_file docs/JUMI_SCHEDULER_BOUNDARY.ko.md

  require_grep 'jumi\.io/run-key' docs/JUMI_K8S_JOB_LABEL_CONTRACT.md \
    "label contract documents run-key"
  require_grep 'jumi\.io/node-key' docs/JUMI_K8S_JOB_LABEL_CONTRACT.md \
    "label contract documents node-key"
  require_grep 'jumi\.io/attempt-id' docs/JUMI_K8S_JOB_LABEL_CONTRACT.md \
    "label contract documents attempt-id"
  require_grep 'user\.jumi\.io/' docs/JUMI_K8S_JOB_LABEL_CONTRACT.md \
    "label contract documents user label prefix"
  require_grep 'kueue\.x-k8s\.io/queue-name' docs/JUMI_K8S_JOB_LABEL_CONTRACT.md \
    "label contract documents Kueue queue label"
  require_grep 'nodeSelector\["kubernetes\.io/hostname"\]' docs/JUMI_K8S_JOB_LABEL_CONTRACT.md \
    "label contract documents requiredNodeName hostname materialization"
  require_grep 'different UID is treated as not found' docs/JUMI_K8S_JOB_LABEL_CONTRACT.md \
    "label contract documents UID mismatch snapshot behavior"
  require_grep 'Pod event whose identity labels do not match' docs/JUMI_K8S_JOB_LABEL_CONTRACT.md \
    "label contract documents stale Pod event rejection"
}

check_spawner_label_contract() {
  echo "== spawner label contract guardrails =="
  local file="pkg/spawner/k8s_jobclient.go"
  local test_file="pkg/spawner/k8s_jobclient_test.go"

  require_grep 'labelRunKey[[:space:]]+= "jumi\.io/run-key"' "$file" \
    "spawner defines run-key label constant"
  require_grep 'labelNodeKey[[:space:]]+= "jumi\.io/node-key"' "$file" \
    "spawner defines node-key label constant"
  require_grep 'labelAttemptID[[:space:]]+= "jumi\.io/attempt-id"' "$file" \
    "spawner defines attempt-id label constant"
  require_grep 'userLabelPrefix[[:space:]]+= "user\.jumi\.io/"' "$file" \
    "spawner defines user label prefix"
  require_grep 'labelKueueQueueName[[:space:]]+= "kueue\.x-k8s\.io/queue-name"' "$file" \
    "spawner defines Kueue queue label constant"
  require_grep 'annotationMarker[[:space:]]+= "jumi\.io/attempt-marker"' "$file" \
    "spawner preserves full attempt marker annotation"
  require_grep 'existingAttemptMarkerMatches' "$file" \
    "spawner keeps idempotency marker compatibility check"
  require_grep 'jobMatchesBackendRef' "$file" \
    "spawner checks backend UID before observing current attempt"
  require_grep 'podWatchLabelSelector' "$file" \
    "spawner builds Pod watch selector from attempt identity"
  require_grep 'podMatchesIdentityLabels' "$file" \
    "spawner filters stale Pod events by attempt identity"

  require_grep 'TestBuildK8sJobAppliesIdentityLabelContract' "$test_file" \
    "unit test covers Job and Pod identity labels"
  require_grep 'TestBuildK8sJobStoresFullAttemptMarkerInAnnotation' "$test_file" \
    "unit test covers full attempt marker annotation"
  require_grep 'TestValidateK8sJobCreateRequestRejectsReservedUserLabel' "$test_file" \
    "unit test covers reserved user label rejection"
  require_grep 'TestValidateK8sJobCreateRequestRejectsRequiredNodeConflict' "$test_file" \
    "unit test covers requiredNodeName/nodeSelector conflict"
  require_grep 'TestSnapshotIgnoresSameNameJobWithDifferentUID' "$test_file" \
    "unit test covers snapshot UID mismatch"
  require_grep 'TestDeleteIgnoresSameNameJobWithDifferentUID' "$test_file" \
    "unit test covers delete UID mismatch"
  require_grep 'TestPodWatchLabelSelectorIncludesAttemptIdentity' "$test_file" \
    "unit test covers Pod watch identity selector"
  require_grep 'TestPodIdentityFilterRejectsStaleAttempt' "$test_file" \
    "unit test covers stale Pod identity filtering"
}

check_spec_validation_contract() {
  echo "== spec validation guardrails =="
  require_grep 'requiredNodeName.*conflicts with nodeSelector' pkg/spec/validate.go \
    "spec validation rejects requiredNodeName/hostname nodeSelector conflicts"
  require_grep 'kueue\.labels.*must use user\.jumi\.io/' pkg/spec/validate.go \
    "spec validation rejects non-user Kueue labels"
  require_grep 'TestValidateExecutableRunSpec_RejectsRequiredNodeSelectorConflict' pkg/spec/validate_test.go \
    "spec test covers placement conflict"
  require_grep 'TestValidateExecutableRunSpec_RejectsReservedKueueLabelPrefix' pkg/spec/validate_test.go \
    "spec test covers reserved Kueue label prefix"
}

check_backend_adapter_contract() {
  echo "== backend adapter guardrails =="
  require_grep 'userLabels\["kueue\.x-k8s\.io/queue-name"\] = node\.Kueue\.QueueName' pkg/backend/spawner_k8s.go \
    "backend adapter maps explicit Kueue queue hints to integration label"
  require_grep 'strings\.HasPrefix\(k, "user\.jumi\.io/"\)' pkg/backend/spawner_k8s.go \
    "backend adapter only forwards user.jumi.io Kueue labels"
  reject_grep 'userLabels\["jumi/run-id"\]' pkg/backend/spawner_k8s.go \
    "backend adapter no longer emits legacy jumi/run-id label"
  reject_grep 'userLabels\["jumi/node-id"\]' pkg/backend/spawner_k8s.go \
    "backend adapter no longer emits legacy jumi/node-id label"
  require_grep 'TestToAttemptRequestDoesNotExposeLegacyJumiLabels' pkg/backend/spawner_k8s_test.go \
    "backend adapter test guards legacy label removal"
}

check_ci_contract() {
  echo "== CI guardrails =="
  require_file .github/workflows/quality-guardrails.yml
  require_grep 'bash hack/quality-guardrails\.sh' .github/workflows/quality-guardrails.yml \
    "quality guardrails workflow runs the guardrail script"
  require_grep 'go mod tidy' .github/workflows/test.yml \
    "test workflow verifies go mod tidy drift"
  require_grep 'git diff --exit-code go\.mod go\.sum' .github/workflows/test.yml \
    "test workflow fails on go.mod/go.sum drift"
  require_grep "git diff --exit-code '\\*\\.go'" .github/workflows/test.yml \
    "test workflow fails on gofmt drift"
  require_grep '/tmp/ah-addsource-tmp' Makefile \
    "sprint baseline creates artifact-handoff TMPDIR"
  require_grep '/tmp/nan-node-contract-tmp' Makefile \
    "sprint baseline creates node-artifact-runtime TMPDIR"
}

check_source_of_truth
check_spawner_label_contract
check_spec_validation_contract
check_backend_adapter_contract
check_ci_contract

if (( failures > 0 )); then
  echo "quality guardrails failed: ${failures} issue(s)"
  exit 1
fi

echo "quality guardrails passed"
