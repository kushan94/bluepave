# Tests

- Gitops layer: rendered end to end in CI (`hack/test-gitops.sh`, ADR-0002), including the
  ExternalSecret, RBAC, route and network policy. The portal's onboarding (`apps/portal.yaml`)
  is rendered with every app file.
- The portal image: `.github/workflows/portal.yml` installs, typechecks and builds it through the
  golden path on every change to `portal/`.
