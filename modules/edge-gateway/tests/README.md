# Tests

- Infra layer: Bicep build and lint under every profile (CI), what-if before merging.
- Gitops layer: rendered end to end in CI (`hack/test-gitops.sh`, ADR-0002): the Gateway, its
  listeners (one per platform hostname), the certificate and the redirect route, then the
  gateway-helm and external-dns charts with the computed values.
- `bluepave validate` rejects two modules claiming one hostname (Go test
  `TestHostnameClaimedTwice`).
