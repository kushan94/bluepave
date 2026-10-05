#!/usr/bin/env bash
# The example apps (examples/), added to this repository as an adopter would (hack/add-example.sh,
# with the repository's platform settings), then checked:
# - no placeholder is left;
# - the onboarding file passes `bluepave validate`;
# - the chart renders for every stage, before the first promotion (no workloads) and after, and
#   its objects validate; every image is the repository and digest Kargo wrote;
# - the workflow calls the golden path for the app;
# - Go examples pass gofmt, vet and their tests.
# The golden path itself (image builds and scans) runs on examples/anvil in CI (ci.yml).
#
# Needs: go, helm, yq, kubeconform.
set -euo pipefail
cd "$(dirname "$0")/.."

added=()
cleanup() {
  [[ ${#added[@]} -eq 0 ]] || rm -rf "${added[@]}"
  rmdir services 2>/dev/null || true
}
trap cleanup EXIT
work=$(mktemp -d)
added+=("$work")
digest=sha256:$(printf '%064d' 0)

for dir in examples/*/; do
  name=$(basename "$dir")
  echo "==> $name"
  hack/add-example.sh "$name" . >/dev/null
  added+=("services/$name" "apps/$name.yaml" ".github/workflows/$name.yml")
  svc="services/$name"

  if leftover=$(grep -rn '__[A-Z_]*__' "$svc" "apps/$name.yaml" ".github/workflows/$name.yml"); then
    echo "placeholders left:"; echo "$leftover"; exit 1
  fi

  go run ./cmd/bluepave validate >/dev/null

  images=$(yq -r '.spec.delivery.images[]' "apps/$name.yaml")
  chart=$(yq -r '.spec.source.path' "apps/$name.yaml")
  helm lint --quiet "$chart" >/dev/null
  for values in "$svc"/deploy/values-*.yaml; do
    helm template "$name" "$chart" -f "$values" >"$work/before.yaml"
    ! grep -qE '^kind: (Deployment|Rollout)$' "$work/before.yaml"
    set_images=()
    for image in $images; do
      set_images+=(--set "images.$image.repository=placeholder.azurecr.io/$name-$image,images.$image.digest=$digest")
    done
    helm template "$name" "$chart" -f "$values" "${set_images[@]}" >"$work/after.yaml"
    for image in $images; do
      grep -q "image: placeholder.azurecr.io/$name-$image@$digest" "$work/after.yaml"
    done
    if other=$(grep -E '^\s+image: ' "$work/after.yaml" | grep -v 'placeholder.azurecr.io/'); then
      echo "images that aren't the app's own (allowed only if listed in policy-kyverno allowedImages):"
      echo "$other"; exit 1
    fi
    kubeconform -strict -summary -ignore-missing-schemas "$work/after.yaml"
  done

  wf=".github/workflows/$name.yml"
  [[ $(yq '.jobs.build.uses' "$wf") == ./.github/workflows/build-app.yml ]]
  [[ $(yq '.jobs.build.with.app' "$wf") == "$name" ]]
  [[ $(yq '.jobs.build.with.app-dir' "$wf") == "$svc" ]]

  if [[ -f $svc/go.mod ]]; then
    (cd "$svc" && [[ -z $(gofmt -l .) ]] && go vet ./... && go test -count=1 ./...)
  fi
done
echo "==> Examples OK"
