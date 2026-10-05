#!/usr/bin/env bash
# Renders the GitOps layer end to end, for every profile, as Argo CD would:
#   bluepave render -> the root chart -> each module's chart -> the upstream charts they install
# and validates every manifest with kubeconform (Kubernetes and CRD schemas). Runs offline
# except for downloading upstream charts and schemas.
#
# Needs: go (or BLUEPAVE=<binary>), helm, yq (mikefarah), kubeconform.
# Usage: hack/test-gitops.sh [discovered.yaml]   (default: the fixture with made-up IDs)
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"
DISCOVERED=${1:-platform/chart/tests/discovered.yaml}
OUT=$(mktemp -d)
trap 'rm -rf "$OUT"' EXIT
if [[ -z ${BLUEPAVE:-} ]]; then
  go build -o "$OUT/bluepave" ./cmd/bluepave
  BLUEPAVE="$OUT/bluepave"
fi

kubeconform_() {
  kubeconform -strict -summary -output text \
    -schema-location default \
    -schema-location 'https://raw.githubusercontent.com/datreeio/CRDs-catalog/main/{{.Group}}/{{.ResourceKind}}_{{.ResourceAPIVersion}}.json' \
    "$@"
}

status=0
for profile in profiles/*.yaml; do
  name=$(basename "$profile" .yaml)
  dir="$OUT/$name"
  mkdir -p "$dir/modules" "$dir/upstream"
  sed "s/^  profile: .*/  profile: $name/" bluepave.yaml > "$dir/bluepave.yaml"
  "$BLUEPAVE" render -f "$dir/bluepave.yaml" -o "$dir/resolved.yaml" >/dev/null

  for env in $(yq -r '.spec.environments // ["dev"] | .[]' "$dir/bluepave.yaml"); do
    echo "== $name/$env: root chart"
    helm template root platform/chart --namespace argocd \
      -f "$dir/bluepave.yaml" -f "$dir/resolved.yaml" -f "$DISCOVERED" \
      --set environment="$env" > "$dir/root.yaml"
    kubeconform_ "$dir/root.yaml" || status=1

    # Each module Application: render its chart with the values the root gave it.
    for app in $(yq -r 'select(.kind == "Application") | .metadata.name' "$dir/root.yaml"); do
      path=$(yq -r "select(.metadata.name == \"$app\") | .spec.source.path" "$dir/root.yaml")
      yq "select(.metadata.name == \"$app\") | .spec.source.helm.valuesObject" "$dir/root.yaml" > "$dir/$app.values.yaml"
      echo "== $name/$env: $app ($path)"
      helm lint --quiet "$path" -f "$dir/$app.values.yaml" >/dev/null || { echo "::error::helm lint $path ($name)"; status=1; }
      helm template "$app" "$path" --namespace argocd -f "$dir/$app.values.yaml" > "$dir/modules/$app.yaml"
      kubeconform_ "$dir/modules/$app.yaml" || status=1

      # Applications the module installs from a Helm repository: render the upstream chart with
      # the computed values, which catches values the chart rejects.
      for sub in $(yq -r 'select(.kind == "Application" and .spec.source.chart != null) | .metadata.name' "$dir/modules/$app.yaml"); do
        q="select(.metadata.name == \"$sub\")"
        repo=$(yq -r "$q | .spec.source.repoURL" "$dir/modules/$app.yaml")
        chart=$(yq -r "$q | .spec.source.chart" "$dir/modules/$app.yaml")
        version=$(yq -r "$q | .spec.source.targetRevision" "$dir/modules/$app.yaml")
        ns=$(yq -r "$q | .spec.destination.namespace" "$dir/modules/$app.yaml")
        yq "$q | .spec.source.helm.valuesObject" "$dir/modules/$app.yaml" > "$dir/upstream/$sub.values.yaml"
        echo "== $name/$env: $app -> $chart $version"
        if [[ $repo == http* ]]; then src=(--repo "$repo" "$chart"); else src=("oci://$repo/$chart"); fi
        helm template "$sub" "${src[@]}" --version "$version" --namespace "$ns" \
          -f "$dir/upstream/$sub.values.yaml" > "$dir/upstream/$sub.yaml"
        kubeconform_ -ignore-missing-schemas "$dir/upstream/$sub.yaml" || status=1
      done
    done
  done
done
exit $status
