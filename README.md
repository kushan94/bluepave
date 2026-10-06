# bluepave

**An Azure-native internal developer platform: a paved road from a repository to a signed,
promoted, running app on AKS.**

You describe your platform in one file, `bluepave.yaml`, and run `bluepave up`. You get an AKS
platform with GitOps, promotion between stages, admission guardrails, a developer portal, and
self-service databases, caches and storage. App teams then ship by opening a pull request.

> Status: **pre-release.** bluepave is extracted from a working platform (Skyforge). Everything
> is covered by unit, render and policy tests; the end-to-end run on a real subscription gates
> v0.1.0.

- [How it works](#how-it-works)
- [What you get](#what-you-get)
- [Getting started](#getting-started)
- [Adding an app](#adding-an-app)
- [Commands](#commands)
- [Repository layout](#repository-layout)

## How it works

![bluepave architecture: GitHub (platform repository, golden path, GitHub App), the Azure subscription (DNS, Entra ID, policy; per environment a network, registry, Key Vault, PostgreSQL, storage, monitoring) and the AKS cluster (Argo CD, Kargo, Kyverno, Falco, Envoy Gateway, cert-manager, kro and Azure Service Operator, observability, app namespaces), with the numbered path of a change](docs/images/architecture.svg)

*Regenerate with `python3 hack/architecture-diagram/generate.py`. Logos come from their official sources and are trademarks of their owners.*

bluepave is a set of **modules**, each one capability: `aks`, `network`, `registry`,
`gitops-argocd`, `delivery-kargo`, `policy-kyverno`, `self-service`, `portal`, and others
([modules/](modules/)). A module can have:
- an **infra layer**: Bicep, deployed as one Azure deployment stack per environment;
- a **GitOps layer**: a Helm chart that Argo CD installs in the cluster;
- docs, tests, and settings with a JSON Schema.

Your **profile** (`trial`, `standard`, `production`) decides which modules are on and how big
things are. `bluepave.yaml` can override both.

**`bluepave up`** runs as you, a subscription Owner, and does in order:
1. **accounts:** the admins group and the CI identity GitHub Actions uses to deploy.
2. **infra:** every module's stack, in dependency order. Outputs (IDs, URLs) go to
   `.bluepave/discovered.yaml`, which you commit; there are no secrets in it.
3. **identities:** Entra apps for Argo CD, Grafana and the portal. Their secrets go to Key Vault.
4. **github-app:** the platform's GitHub App, so Argo CD, Kargo and the portal can read and write
   the repository.
5. **gitops:** installs Argo CD and points it at the platform repository.

From then on **Git is the source of truth**. Argo CD renders the root chart
([platform/chart](platform/chart/)), which creates one Argo CD Application per enabled module and
one per app.

**Apps** are onboarded by a file, `apps/<name>.yaml`. The platform turns it into:
- namespaces `<name>-<stage>`;
- an Argo CD project that confines the app;
- one Argo CD Application per stage;
- a Kargo pipeline.

The **golden path** ([build-app.yml](.github/workflows/build-app.yml)) builds each image, scans
it, and signs it with Cosign. **Kyverno** admits only images signed by that workflow. **Kargo**
deploys each new image to the first stage and promotes it on request, by committing the image
digest to Git.

**Platform APIs** (`AppDatabase`, `AppCache`, `AppStorage`) let an app ask for a backing service
in a few lines of YAML. kro and Azure Service Operator create it, with a workload identity and no
passwords ([ADR-0003](docs/adr/0003-self-service-apis.md)).

The design is in [docs/adr/](docs/adr/): the framework ([0001](docs/adr/0001-framework-architecture.md)),
the GitOps layer ([0002](docs/adr/0002-gitops-layer.md)), the platform APIs
([0003](docs/adr/0003-self-service-apis.md)).

## What you get

- **Azure, as code:** hub-spoke network, AKS (Azure CNI Overlay + Cilium, Entra ID only,
  Workload Identity), Container Registry with per-app permissions, Key Vault, PostgreSQL (Entra
  ID only, private endpoint), DNS, budgets and region policies. No passwords anywhere.
- **The golden path:**
  - checks for Go and Python (tests, gofmt/vet, ruff, govulncheck, pip-audit), gitleaks, Semgrep
    and Trivy;
  - an image with an SBOM and SLSA provenance;
  - a Cosign keyless signature.
- **Guardrails at admission:**
  - only images signed by the golden path;
  - each app runs only its own images;
  - Pod Security `restricted`, default-deny networking, Falco.
- **Delivery:** Argo CD (GitOps), Kargo (dev → staging promotion), Argo Rollouts canaries.
- **Self-service:** one onboarding file per app, and the platform APIs `AppDatabase`, `AppCache`
  and `AppStorage`.
- **A developer portal (Backstage):** catalog, TechDocs, a live Kubernetes view, Entra ID sign-in,
  and templates for new Go and Python services.
- **Observability:** OpenTelemetry → Tempo for traces, managed Prometheus and Grafana for
  metrics.

| Profile | For | Approx. cost |
|---|---|---|
| `trial` | Learning, demos: fits an Azure Free Trial (4 regular + 3 Spot vCPUs) | US$3–5/day |
| `standard` | A team's dev/test platform | tbd |
| `production` | Private cluster, Azure Firewall, private endpoints, zone redundancy | tbd |

## Getting started

**[docs/getting-started.md](docs/getting-started.md)** walks from an empty subscription to a
running platform and a first app. In short:

```bash
gh repo create acme/acme-platform --private --template kushan94/bluepave --clone && cd acme-platform
$EDITOR bluepave.yaml                      # name, prefix, profile, region, domain, GitHub owner
go run ./cmd/bluepave validate             # check it
go run ./cmd/bluepave preflight            # can this subscription run the profile in this region?
go run ./cmd/bluepave up                   # stops at gitops: commit and push bluepave.yaml and .bluepave/
git add bluepave.yaml .bluepave && git commit -m "Platform configuration" && git push
go run ./cmd/bluepave up -step gitops      # Argo CD takes over
go run ./cmd/bluepave status               # stacks, Argo CD applications, URLs
```

Delegate your domain to the name servers `up` prints. When you're done:
`go run ./cmd/bluepave down`.

## Adding an app

An app lives in the platform repository under `services/<name>/`, with its onboarding file in
`apps/<name>.yaml` and its workflow in `.github/workflows/<name>.yml`. You add one with a pull
request, and the platform does the rest after the merge.

### The quick ways

- **From the portal:** Create → "New Go service" or "New Python service". Fill in a name, an
  owner, and whether you want a public route and blob storage. The portal opens the pull request
  for you.
- **The example app:** `hack/add-example.sh anvil` adds [anvil](examples/anvil/): Go, three
  images, and a database, cache and blob storage from the platform APIs. Commit it on a branch
  and open the pull request.

### By hand

**1. The code and its Dockerfile** (`services/greeter/`):
- **Images:** one image per folder in `cmd/`, built with `--build-arg APP=<folder>` and named
  `greeter-<folder>`. Without `cmd/`, the app has a single image, `greeter-web`.
- **Language:** Go (`go.mod`) or Python (`pyproject.toml`, with hash-pinned `requirements.txt`);
  the golden path runs that language's checks.
- **Runtime:** a non-root user and a read-only root filesystem. Pods run under Pod Security
  `restricted`.

**2. A Helm chart** (`services/greeter/deploy/chart/`), with one values file per stage next to
it (`deploy/values-dev.yaml`, `deploy/values-staging.yaml`). Kargo writes each image there:

```yaml
# deploy/values-dev.yaml: Kargo fills these in on every promotion. Keep block style, one key per
# line: Kargo can't edit inline { ... } mappings (bluepave validate checks this).
images:
  api:
    repository: ""
    tag: ""
    digest: ""
```

```yaml
# a Deployment in the chart: pinned by digest, never naming the registry
{{- with .Values.images.api }}{{ if .digest }}
      containers:
        - name: api
          image: {{ .repository }}@{{ .digest }}
{{- end }}{{ end }}
```

The chart also brings its own networking:
- **NetworkPolicies:** namespaces deny by default. Allow ingress from `envoy-gateway-system`
  for a public route, and from `kube-system` for metrics.
- **Public URL:** an `HTTPRoute` to the Gateway `public` in `gateway`, listener `https`, gives
  the app `https://greeter.<env>.<domain>`.

The template's chart
([modules/portal/templates/common](modules/portal/templates/common/)) is a complete starting point.

**3. The onboarding file** (`apps/greeter.yaml`):

```yaml
apiVersion: bluepave.dev/v1alpha1
kind: App
metadata:
  name: greeter
  description: Greets visitors
spec:
  stages:
    - name: dev           # gets every new image automatically
    - name: staging       # promoted by hand in Kargo
  source:
    path: services/greeter/deploy/chart
  delivery:
    images: [api]         # greeter-api, built by the golden path
```

**4. The workflow** (`.github/workflows/greeter.yml`): it calls the golden path with
`app: greeter` and `app-dir: services/greeter`. Copy the template's
([modules/portal/templates/common/.github/workflows](modules/portal/templates/common/.github/workflows/)),
and set its path filters to `services/greeter/**`.

**5. Optional: a catalog entry** (`services/greeter/catalog-info.yaml`), so the portal lists it.

**6. Optional: backing services from the platform APIs**, in the chart:

```yaml
apiVersion: platform.bluepave.dev/v1alpha1
kind: AppDatabase                    # also AppCache (size) and AppStorage (container)
metadata:
  name: orders
  annotations:
    argocd.argoproj.io/sync-options: SkipDryRunOnMissingResource=true
spec:
  serviceAccountName: api            # the pods' service account; gets a trusted identity
```

Each one writes its connection settings to a ConfigMap (`orders-database`, `<name>-cache`,
`<name>-storage`). Read it with `envFrom`, and label the pod
`azure.workload.identity/use: "true"` so it can get Entra tokens.
[modules/self-service](modules/self-service/docs/README.md) documents each API.

Run `go run ./cmd/bluepave validate` before you push: it checks every `apps/*.yaml`.

### What happens after the merge

1. **The golden path runs** on `main`: checks, then each image is built, scanned, pushed with an
   SBOM and provenance, and signed.
2. **Argo CD onboards the app:**
   - namespaces `greeter-dev` and `greeter-staging` (restricted, labelled for the app);
   - the app's Argo CD project;
   - one Application per stage;
   - a quota for the platform APIs.
3. **Kargo deploys to dev:** it sees the new images, commits their digests to
   `deploy/values-dev.yaml`, and Argo CD syncs `greeter-dev`. The admission policy checks every
   image's signature.
4. **You promote to staging** in Kargo when dev looks good; Kargo commits to
   `values-staging.yaml`.

Every later change follows the same path: merge, build and sign, deploy to dev, promote. To
remove an app, delete its namespaces and Kargo project, then its files
([apps/README.md](apps/README.md) has the details and the full contract).

## Commands

| Command | Does |
|---|---|
| `bluepave validate` | Checks `bluepave.yaml`, every module's settings, and `apps/*.yaml` |
| `bluepave modules` | Lists the available modules |
| `bluepave render` | Writes `.bluepave/resolved.yaml` (the modules, for GitOps) and `platform-settings.yaml` (for the portal) |
| `bluepave plan [-what-if]` | Lists the deployment stacks in order, optionally with Azure's what-if |
| `bluepave preflight` | Checks the region offers the profile's VM sizes, zones, quota and PostgreSQL |
| `bluepave up [-step …]` | Creates or updates the platform (see [How it works](#how-it-works)); safe to re-run |
| `bluepave status` | Stacks, Argo CD applications and URLs; non-zero until healthy |
| `bluepave down` | Deletes everything `up` created, after asking for the platform's name |

## Repository layout

```
bluepave.yaml              your platform (the file you edit)
.bluepave/                 generated: resolved modules, platform settings, discovered IDs
profiles/                  trial, standard, production
modules/<name>/            one capability: module.yaml, infra/ (Bicep), gitops/ (Helm), docs/, tests/
platform/chart/            the GitOps root: an Argo CD Application per module and per app
platform/charts/           app onboarding (namespaces, project, stages, Kargo pipeline)
apps/                      onboarding files, one per app
services/                  apps that live in this repository
examples/                  example apps (hack/add-example.sh)
portal/                    the developer portal (Backstage)
api/v1alpha1/              JSON Schemas: platform, profile, module, app
cmd/, internal/            the bluepave CLI (Go)
lib/bicep/                 shared Bicep: configuration, naming, tags
.github/workflows/         CI, and the golden path (build-app.yml)
docs/                      getting started, ADRs, design
```

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) and [SECURITY.md](SECURITY.md). Every pull request runs:
- the CLI tests;
- Bicep builds under every profile;
- the GitOps render (kubeconform) and the policy tests;
- dry runs of the templates and examples;
- the golden path on the fixtures and examples.

## License

[Apache License 2.0](LICENSE).
