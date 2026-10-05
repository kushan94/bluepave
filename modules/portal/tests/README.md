# Tests

- Gitops layer: rendered end to end in CI (`hack/test-gitops.sh`, ADR-0002), including the
  ExternalSecret, RBAC, route and network policy. The portal's onboarding (`apps/portal.yaml`)
  is rendered with every app file.
- The portal image: `.github/workflows/portal.yml` installs, typechecks and builds it through the
  golden path on every change to `portal/`.
- Templates: `hack/test-templates.sh` renders each template like Backstage's `fetch:template`
  (with every option on, then off), then checks the result: no expressions left, the onboarding
  file passes `bluepave validate`, the chart renders and validates for each stage before and
  after a promotion, the workflow calls the golden path, and the code passes the golden path's
  checks for its language.
