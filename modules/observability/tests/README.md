# Tests

- Infra layer: Bicep build and lint under every profile (CI), what-if before merging.
- Gitops layer: rendered end to end in CI (`hack/test-gitops.sh`, ADR-0002): this chart, then the
  opentelemetry-collector, tempo and grafana charts with the computed values. The test fixture
  has every ID, so Entra sign-in and the Prometheus data source render too.
