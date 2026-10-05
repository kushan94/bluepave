#!/usr/bin/env bash
# Checks that every module's declared outputs (module.yaml spec.outputs) are exactly the outputs
# its infra layer produces. `bluepave up` records the template's outputs in discovered.yaml and
# other modules read them by name, so the manifest must not drift from the template.
#
# Needs: bicep (BICEP=<cmd>, default "az bicep"), jq, yq (mikefarah).
set -euo pipefail
cd "$(dirname "$0")/.."
read -r -a BICEP <<<"${BICEP:-az bicep}"

# `az bicep build --file x` and the standalone `bicep build x` take the file differently.
bicep_build() {
  if [[ ${BICEP[0]} == az ]]; then "${BICEP[@]}" build --file "$1" --stdout; else "${BICEP[@]}" build "$1" --stdout; fi
}
status=0
for manifest in modules/*/module.yaml; do
  dir=$(dirname "$manifest")
  infra=$(yq -r '.spec.layers.infra // ""' "$manifest")
  [[ -n $infra ]] || continue
  declared=$(yq -r '(.spec.outputs // [])[]' "$manifest" | sort | tr '\n' ' ')
  actual=$(bicep_build "$dir/$infra" | jq -r '(.outputs // {}) | keys[]' | sort | tr '\n' ' ')
  if [[ $declared == "$actual" ]]; then
    echo "ok   $dir: ${actual:-(none)}"
  else
    echo "::error file=$manifest::outputs differ. module.yaml: [${declared% }], template: [${actual% }]"
    status=1
  fi
done
exit $status
