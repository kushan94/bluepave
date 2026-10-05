#!/usr/bin/env bash
# Runs the Kyverno CLI tests of every module that has them (ADR-0001, quality bar). A module opts in
# with modules/<name>/tests/values.yaml: its gitops chart is rendered with those values, the
# admission policies in the output are written to <tests>/policies.yaml (in a temporary copy), and
# `kyverno test` runs every kyverno-test.yaml under tests/.
#
# Needs: helm, yq (mikefarah), kyverno (CLI, same minor version as the module's Kyverno).
# Usage: hack/test-policies.sh
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"
OUT=$(mktemp -d)
trap 'rm -rf "$OUT"' EXIT

POLICY_KINDS='ValidatingAdmissionPolicy|ValidatingAdmissionPolicyBinding|ValidatingPolicy|ImageValidatingPolicy|MutatingPolicy|ClusterPolicy|Policy'

status=0
found=0
for values in modules/*/tests/values.yaml; do
  [[ -f $values ]] || continue
  module=$(basename "$(dirname "$(dirname "$values")")")
  found=1
  echo "== $module"
  cp -R "modules/$module/tests" "$OUT/$module"
  helm template "$module" "modules/$module/gitops" -f "$values" \
    | yq "select(.kind != null and (.kind | test(\"^($POLICY_KINDS)\$\")))" > "$OUT/$module/policies.yaml"
  kyverno test "$OUT/$module" --remove-color || status=1
done
[[ $found == 1 ]] || echo "no module has policy tests"
exit $status
