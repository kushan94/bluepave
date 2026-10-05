#!/usr/bin/env bash
# The portal's templates (modules/portal/templates), dry run: render each one the way Backstage
# does, with the repository's platform settings (.bluepave/platform-settings.yaml), then check
# what a new service would get:
# - no template expression is left (outside the workflow's own GitHub expressions);
# - the onboarding file passes `bluepave validate`;
# - the chart renders for every stage, with and without an image, and its objects validate;
# - the workflow calls the golden path for the new app;
# - the code passes the golden path's checks for its language (gofmt, vet, tests / ruff, pytest).
#
# Needs: node (npm), go, python3 (3.14), helm, yq, kubeconform.
set -euo pipefail
cd "$(dirname "$0")/.."

work=$(mktemp -d)
cleanup() { rm -rf "$work" apps/demo-svc.yaml; }
trap cleanup EXIT

echo "==> Renderer (nunjucks, pinned by package-lock.json)"
(cd hack/render-template && npm ci --ignore-scripts --no-audit --no-fund --silent)

# The platform settings the portal reads (`bluepave render`, .bluepave/platform-settings.yaml).
settings=.bluepave/platform-settings.yaml
annotation() { yq ".metadata.annotations[\"bluepave.dev/$1\"]" "$settings"; }
owner=$(annotation github-owner)
repo=$(annotation platform-repo)
environment=$(annotation environment)
domain=$(annotation domain)
name=demo-svc
digest=sha256:$(printf '%064d' 0)

for template in modules/portal/templates/*/template.yaml; do
  dir=$(dirname "$template")
  lang=$(basename "$dir")
  component=$(yq '.spec.steps[] | select(.id == "code") | .input.values.component' "$template")
  # Every value the steps pass must be one we set below (renders throw on anything undefined).
  for options in "true true" "false false"; do
    read -r publicRoute storage <<<"$options"
    out="$work/$lang-$publicRoute"
    echo "==> $lang (route $publicRoute, storage $storage) -> image $name-$component"
    jq -n --arg name "$name" --arg owner "$owner" --arg repo "$repo" --arg domain "$domain" --arg environment "$environment" \
      --arg component "$component" --argjson publicRoute "$publicRoute" --argjson storage "$storage" \
      '{name: $name, description: "A \"demo\" service: tests the template", owner: "group:default/platform-team",
        publicRoute: $publicRoute, storage: $storage, component: $component,
        githubOwner: $owner, platformRepo: $repo, environment: $environment, domain: $domain}' >"$work/values.json"
    # The keys the template passes are exactly the ones the test sets.
    diff <(yq -o=json '.spec.steps[] | select(.action == "fetch:template") | .input.values | keys' "$template" | jq -c 'sort' | sort -u) \
      <(jq -c 'keys | sort' "$work/values.json")
    for part in "$dir/skeleton" modules/portal/templates/common; do
      node hack/render-template/render.mjs "$part" "$out" "$work/values.json"
    done

    svc="$out/services/$name"
    if leftover=$(grep -rn '\${{\|{%' "$out" --exclude-dir=.github); then
      echo "unrendered template expressions:"; echo "$leftover"; exit 1
    fi

    # Onboarding: the CLI's own validation, with the file in place.
    cp "$out/apps/$name.yaml" apps/
    go run ./cmd/bluepave validate >/dev/null
    rm apps/$name.yaml
    [[ $(yq '.spec.source.path' "$out/apps/$name.yaml") == "services/$name/deploy/chart" ]]
    [[ $(yq ".spec.delivery.images[0]" "$out/apps/$name.yaml") == "$component" ]]

    # Chart: every stage, before the first promotion (no Deployment) and after.
    helm lint --quiet "$svc/deploy/chart" >/dev/null
    for values in "$svc"/deploy/values-*.yaml; do
      helm template "$name" "$svc/deploy/chart" -f "$values" >"$work/before.yaml"
      ! grep -q '^kind: Deployment' "$work/before.yaml"
      yq -i ".images.$component = {\"repository\": \"placeholder.azurecr.io/$name-$component\", \"tag\": \"abc\", \"digest\": \"$digest\"}" "$values"
      helm template "$name" "$svc/deploy/chart" -f "$values" >"$work/after.yaml"
      grep -q "image: placeholder.azurecr.io/$name-$component@$digest" "$work/after.yaml"
      [[ $(grep -c '^kind: HTTPRoute' "$work/after.yaml" || true) == $([[ $publicRoute == true ]] && echo 1 || echo 0) ]]
      [[ $(grep -c '^kind: AppStorage' "$work/after.yaml" || true) == $([[ $storage == true ]] && echo 1 || echo 0) ]]
      kubeconform -strict -summary -ignore-missing-schemas "$work/after.yaml"
    done

    # Workflow: the golden path for this app, from its folder.
    wf="$out/.github/workflows/$name.yml"
    [[ $(yq '.jobs.build.uses' "$wf") == ./.github/workflows/build-app.yml ]]
    [[ $(yq '.jobs.build.with.app' "$wf") == "$name" ]]
    [[ $(yq '.jobs.build.with.app-dir' "$wf") == "services/$name" ]]
    [[ $(yq '.jobs.build.with.publish' "$wf") == *'github.event_name'* ]]
    yq '.' "$svc/catalog-info.yaml" >/dev/null

    # Code: the golden path's checks for the language (build-app.yml).
    if [[ $publicRoute == true ]]; then
      case $lang in
        go-service)
          (cd "$svc" && [[ -z $(gofmt -l .) ]] && go vet ./... && go test -count=1 ./...)
          ;;
        python-service)
          python3 -m venv "$work/venv"
          "$work/venv/bin/pip" install --quiet --require-hashes -r "$svc/requirements.txt" -r "$svc/requirements-dev.txt"
          (cd "$svc" && "$work/venv/bin/ruff" check . && "$work/venv/bin/ruff" format --check . && "$work/venv/bin/pytest" -q)
          ;;
      esac
    fi
  done
done
echo "==> Templates OK"
