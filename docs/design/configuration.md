# Design: from one platform instance to an installable product

bluepave is extracted from Skyforge, a working platform built for one Azure subscription and one
GitHub account. This document is the plan for making it installable by anyone. It covers the
inventory of what's tied to the original instance, the single configuration file that replaces
it, how each layer reads that file, and the order of work.

## 1. Inventory: what's tied to one instance today

Counted in the Skyforge repository (excluding lock files and dependencies):

| Value | Occurrences | Mostly in | Becomes |
|---|---:|---|---|
| GitHub owner (`kushan94`) | 110 | platform (53), apps (40) | `github.owner` |
| Domain (`skyforgelab.cloud`) | 104 | platform (54), apps (20) | `dns.domain` |
| Naming prefix (`cnp`) | 164 | infra (62), scripts (43) | `prefix` |
| Region code (`ea`) | 50 | platform, infra, scripts | derived from `azure.region` |
| Registry, Key Vault, PostgreSQL names | 34 | apps, platform | derived (prefix + env + region + hash) |
| Admin group name / object ID | 27 / 5 | platform, scripts | `admins.group` / discovered |
| Subscription and tenant IDs | 17 | platform | discovered |
| Client IDs, GitHub App IDs, OIDC issuer | 9 | platform, portal | discovered |
| A person's object ID and email | 1 | platform (catalog) | removed: users come from Entra ID |
| Example app names (`anvil`, `greeter`, `pulse`) | 254 | apps | `examples/`, not part of the platform |

Two layers need different treatment:
- **Infrastructure (Bicep)** is already parameterised through one typed config file. It can read
  `bluepave.yaml` directly with `loadYamlContent()`.
- **Platform (GitOps manifests)** is applied by Argo CD as plain files, so the literals live there.
  This is the main refactor.

## 2. One configuration file

```yaml
# bluepave.yaml: everything an adopter decides. Nothing here is secret.
prefix: bp                      # resource names: rg-bp-dev-ea-aks, ...
profile: trial                  # trial | standard | production
azure:
  region: eastasia
environments: [dev]             # dev, staging stage lives inside dev; prod is a separate cluster
dns:
  domain: example.com           # <app>.<env>.example.com; the zone is created in Azure
github:
  owner: acme                   # user or organisation that owns the platform and app repositories
  platformRepo: bluepave        # this repository, as forked or templated
  appName: acme-bluepave        # the platform's GitHub App (created by `bluepave up`)
admins:
  group: platform-admins        # Entra ID group: cluster admins, portal users, Kargo promoters
features:
  portal: true
  appStorage: true
  observability: true
  falco: true
  firewall: false               # production profile: on
```

**Discovered values never go in this file.** Subscription, tenant, object and client IDs are
written by `bluepave up` to `.bluepave/discovered.yaml` and committed, so GitOps can read them.
They're identifiers, not secrets. Secrets stay in Key Vault.

## 3. How each layer reads it

| Layer | Mechanism |
|---|---|
| Bicep (module infra layers) | `lib/bicep/platform.bicep` reads `bluepave.yaml`, the profiles and `.bluepave/discovered.yaml` with `loadYamlContent`; modules import names, tags and settings from it. |
| GitOps | The root chart `platform/chart` renders one Argo CD Application per enabled module from `bluepave.yaml`, `.bluepave/resolved.yaml` and `.bluepave/discovered.yaml`; each module's chart gets the module context ([ADR-0002](../adr/0002-gitops-layer.md)). |
| Policies | The Kyverno signer identity and the registry prefix come from chart values. Releases pin the signer to `build-app.yml@refs/tags/v<major>.*`. |
| Golden path | `build-app.yml` takes the registry, tenant and subscription as inputs. Adopters call `<owner>/<platformRepo>/.github/workflows/build-app.yml@v1`. |
| Portal | A generic, signed image. Configuration (domain, owner, Entra app, catalog locations) is mounted as a ConfigMap rendered from the values. Templates take the owner and repository from the same values. |
| CLI | `bluepave` reads `bluepave.yaml`, preflights, bootstraps, discovers, writes `.bluepave/discovered.yaml`. |

## 4. Repository layout

```
bluepave.yaml                 adopter's configuration (the only file to edit)
.bluepave/discovered.yaml     IDs written by `bluepave up` (committed)
api/v1alpha1/                 the config API: JSON Schemas for bluepave.yaml, profiles, modules
profiles/                     trial, standard, production
cmd/bluepave/, internal/      the bluepave CLI (Go)
lib/bicep/                    shared Bicep library: config, naming, tags (every infra layer imports it)
modules/<name>/               one capability: module.yaml, infra/, gitops/, policies/, portal/, docs/, tests/
platform/chart/               the GitOps root: one Argo CD Application per enabled module
portal/                       Backstage (generic image)
.github/workflows/            CI, build-app (the golden path)
examples/                     anvil (Go, three services), pulse (Python), external-repo example
docs/                         getting started, architecture, ADRs, runbooks, troubleshooting
```

## 5. The CLI

| Command | Does |
|---|---|
| `bluepave preflight` | Checks the tools, Azure rights (Owner, permission to create app registrations), the vCPU quota for the profile in the region, provider registrations, and GitHub access. |
| `bluepave up` | Creates the CI identity and GitHub OIDC trust, the admin group (if missing), the platform GitHub App (manifest flow, one browser step), the portal's Entra app, and the DNS zone (prints the name servers to set at the registrar). Writes discovered IDs, runs the first deployment, and bootstraps Argo CD. Idempotent: re-running converges. |
| `bluepave status` | Shows stack, cluster, Argo CD and Kargo health, and the URLs. |
| `bluepave down` | Deletes the deployment stacks, app registrations and the GitHub App. Asks first, and lists what will go. |

## 6. Profiles

The Skyforge dev environment *is* the `trial` profile, including its constraints:
- one system node, apps on Spot;
- manual upgrades, because the upgrade surge doesn't fit the quota;
- no NAT gateway or firewall.

`standard` and `production` turn on what Skyforge's prod config describes. That config has
**never been deployed**, so it needs an end-to-end test before it's advertised.

## 7. What gets removed in the extraction

- **Transition workarounds:** the Kyverno rule accepting an old GitHub account name and the old
  `app.yml` signer, and the images copied during the app rename.
- **Personal data:** the catalog's user entry (users come from Entra ID, by group).
- **Single-user shortcuts become profile choices:** portal access assigned to one user (needs Entra
  ID P1 for groups), Spot-only apps.
- **Git history:** bluepave starts with a fresh history.

## 8. Order of work

1. **Configuration:** `bluepave.yaml` schema and validation; Bicep reads it; profiles.
2. **GitOps chart:** `platform/chart` and the module context (done, ADR-0002).
2b. **App onboarding:** `apps/<name>.yaml` (`api/v1alpha1/app.schema.json`), the
    `platform/charts/app-onboarding` chart and the root chart's `apps` ApplicationSet (done).
3. **CLI:** preflight, up, status, down, folding in the seven Skyforge bootstrap scripts.
4. **Generic portal image** with mounted configuration.
5. **Examples and docs:** getting started, runbooks, troubleshooting.
6. **End-to-end test:** a fresh subscription → `up` → examples → `down`, nightly.
7. **Release:** license, SECURITY.md, CONTRIBUTING, v0.1.0, make the repository public.

### Modules to port (target set)

Done: `governance`, `monitoring`, `dns`, `network`, `registry`, `keyvault`, `aks`, `data-postgres`, `gitops-argocd`, `secrets-external`, `certificates`, `delivery-kargo`, `rollouts`, `policy-kyverno`, `runtime-falco`, `edge-gateway`, `observability`, `self-service` (AppStorage; AppCache and AppDatabase next). To do:
`portal`; `self-service` AppCache and AppDatabase ([ADR-0003](../adr/0003-self-service-apis.md)). Each is added to the profiles that should enable it by default
when it lands.

## 9. Open decisions

Decided:
- **License:** Apache-2.0.
- **Distribution:** a GitHub template repository first; product and instance repositories later
  (ADR-0001).
- **Framework structure:** modules behind a versioned config API (ADR-0001).
- **GitOps layer:** a root chart, one Application per module, and the module context (ADR-0002).
- **App resources:** platform APIs on ASO + kro; no platform-wide Redis; Crossplane can replace
  the engine behind the same APIs (ADR-0003).
