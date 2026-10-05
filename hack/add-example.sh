#!/usr/bin/env bash
# Adds an example app (examples/<name>) to this platform repository, the way the portal's templates
# add a service: its code and chart to services/<name>/, its onboarding to apps/<name>.yaml and
# its workflow to .github/workflows/<name>.yml. The placeholders __GITHUB_OWNER__,
# __PLATFORM_REPO__, __ENVIRONMENT__ and __DOMAIN__ are filled in from the platform settings
# (`bluepave render`, .bluepave/platform-settings.yaml). Commit the result and open a pull request.
#
# Needs: yq (mikefarah).
# Usage: hack/add-example.sh <name> [repository root, default .]
set -euo pipefail
name=${1:?usage: hack/add-example.sh <name> [repository root]}
src="$(cd "$(dirname "$0")/.." && pwd)/examples/$name"
root=${2:-.}
settings="$root/.bluepave/platform-settings.yaml"

[[ -d $src ]] || { echo "no example $name (examples/$name)" >&2; exit 1; }
[[ -f $settings ]] || { echo "$settings is missing: run bluepave render" >&2; exit 1; }
for target in "$root/services/$name" "$root/apps/$name.yaml" "$root/.github/workflows/$name.yml"; do
  [[ ! -e $target ]] || { echo "$target already exists" >&2; exit 1; }
done

annotation() { yq ".metadata.annotations[\"bluepave.dev/$1\"]" "$settings"; }
owner=$(annotation github-owner)
repo=$(annotation platform-repo)
environment=$(annotation environment)
domain=$(annotation domain)

mkdir -p "$root/services" "$root/apps" "$root/.github/workflows"
cp -R "$src" "$root/services/$name"
mv "$root/services/$name/platform/app.yaml" "$root/apps/$name.yaml"
mv "$root/services/$name/platform/workflow.yml" "$root/.github/workflows/$name.yml"
rmdir "$root/services/$name/platform"

files=$(grep -rl '__GITHUB_OWNER__\|__PLATFORM_REPO__\|__ENVIRONMENT__\|__DOMAIN__' \
  "$root/services/$name" "$root/apps/$name.yaml" "$root/.github/workflows/$name.yml" || true)
for f in $files; do
  sed -e "s|__GITHUB_OWNER__|$owner|g" -e "s|__PLATFORM_REPO__|$repo|g" \
    -e "s|__ENVIRONMENT__|$environment|g" -e "s|__DOMAIN__|$domain|g" "$f" >"$f.tmp"
  mv "$f.tmp" "$f"
done

echo "added $name: services/$name/, apps/$name.yaml, .github/workflows/$name.yml"
