# Tests

- Infra layer: Bicep build and lint under every profile (CI), what-if before merging.
- Gitops layer: rendered end to end in CI (`hack/test-gitops.sh`, ADR-0002): the module chart,
  then the kargo chart from its OCI registry with the computed values. The ExternalSecrets are
  validated against their CRD schema.
