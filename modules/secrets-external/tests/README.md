# Tests

- Infra layer: Bicep build and lint under every profile (CI), what-if before merging.
- Gitops layer: rendered end to end in CI (`hack/test-gitops.sh`, ADR-0002), including the
  `ClusterSecretStore` (the test fixture has a vault URI).
