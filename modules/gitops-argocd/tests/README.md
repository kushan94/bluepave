# Tests

The gitops layer is rendered end to end in CI (`hack/test-gitops.sh`, ADR-0002):
- the root chart, then this module's chart, then the argo-cd chart it installs, for every
  profile;
- every manifest is checked with kubeconform;
- discovered IDs come from `platform/chart/tests/discovered.yaml`, so the Entra ID sign-in and
  RBAC paths render too.
