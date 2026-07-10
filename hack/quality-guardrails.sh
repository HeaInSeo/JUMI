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
  require_file docs/JUMI_ARTIFACT_MATERIALIZATION_CONTRACT.md
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
  require_grep 'input_materialization_contract_invalid' docs/JUMI_ARTIFACT_MATERIALIZATION_CONTRACT.md \
    "materialization contract documents fail-fast reason"
  require_grep 'preferred_node' docs/JUMI_ARTIFACT_MATERIALIZATION_CONTRACT.md \
    "materialization contract documents preferred locality"
  require_grep 'required_node' docs/JUMI_ARTIFACT_MATERIALIZATION_CONTRACT.md \
    "materialization contract documents required locality"
  require_grep 'local_reuse' docs/JUMI_ARTIFACT_MATERIALIZATION_CONTRACT.md \
    "materialization contract documents local reuse"
  require_grep 'remote_fetch' docs/JUMI_ARTIFACT_MATERIALIZATION_CONTRACT.md \
    "materialization contract documents remote fetch"
  require_grep 'Post-Scheduling Resolve' docs/JUMI_ARTIFACT_MATERIALIZATION_CONTRACT.md \
    "materialization contract documents post-scheduling resolve"
  require_grep 'observation-only' docs/JUMI_ARTIFACT_MATERIALIZATION_CONTRACT.md \
    "materialization contract documents post-scheduling observation-only scope"
  require_grep 'runtime materializer contract' docs/JUMI_ARTIFACT_MATERIALIZATION_CONTRACT.md \
    "materialization contract documents runtime materializer env contract"
  require_grep '_EXPECTED_SIZE_BYTES' docs/JUMI_ARTIFACT_MATERIALIZATION_CONTRACT.md \
    "materialization contract documents expected size env suffix"
  require_grep '_NODE_LOCAL_PATH' docs/JUMI_ARTIFACT_MATERIALIZATION_CONTRACT.md \
    "materialization contract documents node-local path env suffix"
}

check_spawner_label_contract() {
  echo "== spawner label contract guardrails =="
  local file="pkg/spawner/k8s_jobclient.go"
  local test_file="pkg/spawner/k8s_jobclient_test.go"
  local fixture_file="pkg/spawner/testdata/k8s-job-main-attempt.golden.yaml"

  require_file "$fixture_file"
  require_grep 'APIVersion: "batch/v1"' "$file" \
    "spawner renders Job TypeMeta apiVersion"
  require_grep 'Kind:[[:space:]]+"Job"' "$file" \
    "spawner renders Job TypeMeta kind"
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
  require_grep 'TestRenderedK8sJobGoldenFixture' "$test_file" \
    "unit test guards rendered Job YAML fixture drift"
  require_grep 'UPDATE_GOLDEN=1 go test ./pkg/spawner -run TestRenderedK8sJobGoldenFixture' "$test_file" \
    "golden fixture has explicit update command"
  require_grep 'apiVersion: batch/v1' "$fixture_file" \
    "rendered Job fixture includes apiVersion"
  require_grep 'kind: Job' "$fixture_file" \
    "rendered Job fixture includes kind"
  require_grep 'jumi\.io/run-key' "$fixture_file" \
    "rendered Job fixture includes run-key label"
  require_grep 'jumi\.io/node-key' "$fixture_file" \
    "rendered Job fixture includes node-key label"
  require_grep 'jumi\.io/attempt-id' "$fixture_file" \
    "rendered Job fixture includes attempt-id label"
  require_grep 'JUMI_INPUT_ALIGNED_BAM_MATERIALIZATION_MODE' "$fixture_file" \
    "rendered Job fixture includes materialization env"
  require_grep 'JUMI_INPUT_ALIGNED_BAM_EXPECTED_DIGEST' "$fixture_file" \
    "rendered Job fixture includes expected digest env"
  require_grep 'JUMI_INPUT_ALIGNED_BAM_EXPECTED_SIZE_BYTES' "$fixture_file" \
    "rendered Job fixture includes expected size env"
  require_grep 'JUMI_INPUT_ALIGNED_BAM_LOCAL_PATH' "$fixture_file" \
    "rendered Job fixture includes local path env"
  require_grep 'preferredDuringSchedulingIgnoredDuringExecution' "$fixture_file" \
    "rendered Job fixture includes preferred node affinity"
  require_grep 'persistentVolumeClaim' "$fixture_file" \
    "rendered Job fixture includes PVC volume"
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
  require_grep 'contractInputEnvSuffixes = \[\]string' pkg/backend/spawner_k8s.go \
    "backend adapter has explicit node contract input env suffix allowlist"
  require_grep '"_MATERIALIZATION_MODE"' pkg/backend/spawner_k8s.go \
    "backend adapter parses materialization mode env"
  require_grep '"_NODE_LOCAL_PATH"' pkg/backend/spawner_k8s.go \
    "backend adapter parses node-local path env"
  require_grep 'TestBuildNodeContractInputs_FullInput' pkg/backend/pure_logic_test.go \
    "backend test covers full materialization node contract input"
}

check_materialization_contract() {
  echo "== artifact materialization guardrails =="
  local file="pkg/executor/executor.go"
  local test_file="pkg/executor/executor_test.go"

  require_grep 'materializationEnvPrefix[[:space:]]+= "JUMI_INPUT_"' "$file" \
    "executor defines materialization env prefix"
  require_grep 'materializationEnvSuffixMaterializationMode[[:space:]]+= "_MATERIALIZATION_MODE"' "$file" \
    "executor defines materialization mode env suffix"
  require_grep 'materializationEnvSuffixExpectedDigest[[:space:]]+= "_EXPECTED_DIGEST"' "$file" \
    "executor defines expected digest env suffix"
  require_grep 'materializationEnvSuffixExpectedSizeBytes[[:space:]]+= "_EXPECTED_SIZE_BYTES"' "$file" \
    "executor defines expected size env suffix"
  require_grep 'materializationEnvSuffixNodeLocalPath[[:space:]]+= "_NODE_LOCAL_PATH"' "$file" \
    "executor defines node-local path env suffix"
  require_grep 'materializationEnvKey' "$file" \
    "executor builds materialization env keys through helper"
  require_grep 'validateResolvedBindingContract' "$file" \
    "executor validates resolved materialization contract"
  require_grep 'input_materialization_contract_invalid' "$file" \
    "executor fails invalid materialization contracts with stable reason"
  require_grep 'placementModePreferredNode[[:space:]]+= "preferred_node"' "$file" \
    "executor defines preferred_node placement mode"
  require_grep 'placementModeRequiredNode[[:space:]]+= "required_node"' "$file" \
    "executor defines required_node placement mode"
  require_grep 'materializationModeLocalReuse[[:space:]]+= "local_reuse"' "$file" \
    "executor defines local_reuse materialization mode"
  require_grep 'materializationModeRemoteFetch[[:space:]]+= "remote_fetch"' "$file" \
    "executor defines remote_fetch materialization mode"
  require_grep 'TestValidateResolvedBindingContractAcceptsPreferredRemoteFetch' "$test_file" \
    "unit test covers preferred remote_fetch contract"
  require_grep 'TestValidateResolvedBindingContractRejectsPlacementWithoutNode' "$test_file" \
    "unit test covers placement nodeName requirement"
  require_grep 'TestValidateResolvedBindingContractRejectsLocalReuseWithoutNodeLocalSource' "$test_file" \
    "unit test covers local_reuse source requirement"
  require_grep 'TestValidateResolvedBindingContractRejectsRemoteFetchWithoutSource' "$test_file" \
    "unit test covers remote_fetch source requirement"
  require_grep 'TestValidateResolvedBindingContractRejectsUnsafeLocalPath' "$test_file" \
    "unit test covers localPath safety"
  require_grep 'TestInjectResolvedBindingEnvRemoteFetchContract' "$test_file" \
    "unit test covers remote_fetch runtime env contract"
  require_grep 'TestInjectResolvedBindingEnvLocalReuseContract' "$test_file" \
    "unit test covers local_reuse runtime env contract"
}

check_ci_contract() {
  echo "== CI guardrails =="
  require_file .github/workflows/quality-guardrails.yml
  require_file .github/workflows/kube-linter.yml
  require_file .github/workflows/semgrep.yml
  require_grep 'bash hack/quality-guardrails\.sh' .github/workflows/quality-guardrails.yml \
    "quality guardrails workflow runs the guardrail script"
  require_grep '\.github/workflows/kube-linter\.yml' .github/workflows/quality-guardrails.yml \
    "quality guardrails workflow runs when kube-linter workflow changes"
  require_grep '\.github/workflows/semgrep\.yml' .github/workflows/quality-guardrails.yml \
    "quality guardrails workflow runs when Semgrep workflow changes"
  require_grep 'deploy/k8s/\*\*' .github/workflows/quality-guardrails.yml \
    "quality guardrails workflow runs when deploy manifests change"
  require_grep 'go install golang\.stackrox\.io/kube-linter/cmd/kube-linter@v0\.8\.3' .github/workflows/kube-linter.yml \
    "kube-linter workflow installs pinned kube-linter into project bin"
  require_grep 'make kube-linter' .github/workflows/kube-linter.yml \
    "kube-linter workflow runs Makefile target"
  require_grep '\.semgrep/\*\*' .github/workflows/quality-guardrails.yml \
    "quality guardrails workflow runs when Semgrep rules change"
  require_grep 'semgrep --test \.semgrep/rules' .github/workflows/semgrep.yml \
    "Semgrep workflow validates rule fixtures"
  require_grep 'semgrep scan --config \.semgrep/rules --error' .github/workflows/semgrep.yml \
    "Semgrep workflow runs custom rules as blocking checks"
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

check_semgrep_contract() {
  echo "== Semgrep guardrails =="
  require_file .semgrepignore
  require_file .semgrep/rules/jumi-no-direct-podspec-nodename.yaml
  require_file .semgrep/rules/jumi-no-job-name-only-pod-watch.yaml
  require_file .semgrep/rules/jumi-no-job-delete-without-uid-preconditions.yaml
  require_grep 'jumi-no-direct-podspec-nodename' .semgrep/rules/jumi-no-direct-podspec-nodename.yaml \
    "Semgrep blocks direct PodSpec.NodeName binding"
  require_grep 'jumi-no-job-name-only-pod-watch' .semgrep/rules/jumi-no-job-name-only-pod-watch.yaml \
    "Semgrep blocks job-name-only Pod watches"
  require_grep 'jumi-no-job-delete-without-uid-preconditions' .semgrep/rules/jumi-no-job-delete-without-uid-preconditions.yaml \
    "Semgrep blocks Job delete without UID preconditions"
  require_grep 'SEMGREP \?= \$\(LOCALBIN\)/semgrep' Makefile \
    "Makefile uses project-local Semgrep binary"
  require_grep 'KUBE_LINTER \?= \$\(LOCALBIN\)/kube-linter' Makefile \
    "Makefile uses project-local kube-linter binary"
  require_grep '"\$\(SEMGREP\)" scan --config \.semgrep/rules --error' Makefile \
    "Makefile exposes blocking Semgrep scan"
  require_grep '"\$\(SEMGREP\)" --test \.semgrep/rules' Makefile \
    "Makefile exposes Semgrep fixture tests"
  require_grep '"\$\(KUBE_LINTER\)" lint deploy/k8s' Makefile \
    "Makefile exposes blocking kube-linter scan"
}

check_source_of_truth
check_spawner_label_contract
check_spec_validation_contract
check_backend_adapter_contract
check_materialization_contract
check_ci_contract
check_semgrep_contract

if (( failures > 0 )); then
  echo "quality guardrails failed: ${failures} issue(s)"
  exit 1
fi

echo "quality guardrails passed"
