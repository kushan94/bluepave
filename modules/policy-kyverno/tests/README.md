# Tests

- **Policies:** `hack/test-policies.sh` (CI) renders this module's chart with `values.yaml` and
  runs the Kyverno CLI tests:
  - `allowed-images/`: own images and `allowedImages` pass; another app's image, another
    registry, and a disallowed init container are refused.
- Signature verification needs a real registry and signed images, so it's covered by the
  end-to-end test, not here.
- **Infra layer:** Bicep build and lint under every profile (CI), what-if before merging.
- **Gitops layer:** rendered end to end in CI (`hack/test-gitops.sh`, ADR-0002), then the kyverno
  chart with the computed values.
