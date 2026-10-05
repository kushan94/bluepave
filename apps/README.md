# Apps

One file per app, `apps/<name>.yaml`, onboards it onto the platform. Merge it, and the platform
creates, on each cluster that runs one of its stages:
- **Namespaces** `<name>-<stage>`: Pod Security `restricted`, and labelled
  `platform.bluepave.dev/app` and `/stage`. Admission policies and the public Gateway select on
  those labels. Argo CD never deletes these namespaces.
- **The app's Argo CD project:** only its own namespaces and repository. It allows no
  cluster-scoped kinds, raw Azure Service Operator or kro objects, ExternalSecrets, quotas or
  limit ranges.
- **One Argo CD Application per stage**, rendering the app's chart with `values-<stage>.yaml`.
- **A quota** on the platform APIs (with `self-service`), for example at most two `AppStorage`
  per namespace.
- **A Kargo pipeline** (with `delivery-kargo`), with these parts:
  - A Warehouse watches `<registry>/<name>-<image>` for each image. It creates Freight only when
    every image comes from the same commit.
  - The first stage gets each new Freight automatically; later stages are promoted by hand, by
    the platform admins group.
  - A promotion commits `images.<image>.repository`, `.tag` and `.digest` to the stage's values
    file in the app's repository (as the platform's GitHub App), then syncs the stage. App charts
    reference `{{ .Values.images.<image>.repository }}@{{ .Values.images.<image>.digest }}` and
    never name the registry.

```yaml
# apps/greeter.yaml
apiVersion: bluepave.dev/v1alpha1
kind: App
metadata:
  name: greeter
  description: Greets visitors
spec:
  stages:
    - name: dev
    - name: staging
    # - { name: prod, environment: prod }   # a stage on the prod cluster
  source:
    repoURL: https://github.com/<owner>/greeter   # must belong to the platform's GitHub owner
    path: deploy/chart                           # values-<stage>.yaml live in deploy/
  delivery:
    images: [api]                                # built by the golden path as <name>-api
```

## Building images: the golden path

The app's repository calls the platform's reusable workflow, which runs the checks, builds,
scans and, on `main`, publishes and signs:

```yaml
# .github/workflows/build.yml in the app's repository
on:
  pull_request:
  push:
    branches: [main]
jobs:
  build:
    uses: <owner>/<platformRepo>/.github/workflows/build-app.yml@main   # or a release tag, @v1
    permissions: { contents: read, id-token: write }
    with:
      app: greeter
      publish: ${{ github.event_name == 'push' && github.ref == 'refs/heads/main' }}
      registry: ${{ vars.BLUEPAVE_REGISTRY }}
      azure-tenant-id: ${{ vars.BLUEPAVE_TENANT_ID }}
      azure-subscription-id: ${{ vars.BLUEPAVE_SUBSCRIPTION_ID }}
      azure-client-id: ${{ vars.BLUEPAVE_CLIENT_ID }}
```

- **Layout:** a `Dockerfile` taking build arg `APP=<component>`, and one image per folder in
  `cmd/`. Without `cmd/`, the app has a single image, `<name>-web`.
- **Checks** run by what the app has:
  - Go: gofmt, vet, tests, govulncheck;
  - Python: ruff, pytest, pip-audit, hashed requirements;
  - always: gitleaks, Semgrep, Trivy (dependencies, Dockerfile, secrets, the rendered chart).
- **Image:** pushed with an SBOM and SLSA provenance, scanned again, then signed with Cosign
  keyless. Only images signed by this workflow (at `main` or a release tag) are admitted
  (`policy-kyverno`).

The full contract is [api/v1alpha1/app.schema.json](../api/v1alpha1/app.schema.json). Run
`bluepave validate` before merging: it checks every app file.

**Removing an app** is deliberate. Delete its namespaces and Kargo project, then its file. Deleting
only the file leaves everything running (the ApplicationSet preserves resources).

**Not yet supported:**
- promotion across clusters (each cluster's pipeline covers its own stages);
- custom Kargo pipelines (for example with verification steps).
