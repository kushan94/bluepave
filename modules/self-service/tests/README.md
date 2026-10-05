# Tests

- **Infra layer:** Bicep build and lint under every profile (CI), what-if before merging. The RBAC
  Administrator condition was compared with the one running in the reference instance and is
  identical.
- **Gitops layer:** rendered end to end in CI (`hack/test-gitops.sh`, ADR-0002), including the
  ResourceGraphDefinition, then the azure-service-operator and kro charts with the computed
  values.
- **Conformance (ADR-0003, end-to-end test):** for each API, create it, connect from a pod with
  the app's workload identity, delete it and check the data is kept, and check the quota.
