# Getting started

From an empty Azure subscription to a running platform, then a first app on it. Plan about an
hour, most of it waiting for Azure.

> bluepave is pre-release. Every step below is covered by unit and render tests; the full run on
> a real subscription is the end-to-end test that gates v0.1.0.

## 1. What you need

| | |
|---|---|
| **Azure subscription** | You're its **Owner** (the platform assigns roles, including to its own operators). An Azure Free Trial works with the `trial` profile: it fits the trial's quota of 4 regular and 3 Spot vCPUs per region. |
| **Microsoft Entra ID** | Permission to create app registrations and security groups (the default for members of most tenants). No Entra ID P1 is needed. |
| **A domain** | One you can delegate to Azure DNS, e.g. `platform.example.com`. Apps get `<app>.<env>.<domain>`, with Let's Encrypt certificates. |
| **GitHub** | A user or organisation for the platform repository. You need admin rights on it. |
| **Tools** | `az` (signed in), `gh` (signed in), `git`, `kubectl`, `kubelogin`, `helm`, and Go 1.27+ to run the CLI until releases ship binaries. |

**Cost:** about US$3–5 a day on `trial`. Run `bluepave down` when you're done experimenting.

## 2. Create your platform repository

Create a repository from bluepave ("Use this template" on GitHub, or clone and push to a new
repository), then clone it. Everything below runs in it.

```bash
gh repo create acme/acme-platform --private --template kushan94/bluepave --clone
cd acme-platform
```

## 3. Describe your platform: `bluepave.yaml`

It's the only file you edit. The schema is `api/v1alpha1/platform.schema.json`; your editor can
use it for completion.

```yaml
apiVersion: bluepave.dev/v1alpha1
kind: Platform
metadata:
  name: acme                      # shown in the portal; lowercase
spec:
  prefix: acme                    # 2-5 characters, in every Azure resource name
  profile: trial                  # trial | standard | production
  azure:
    region: westeurope
  environments: [dev]             # clusters: dev, and prod for a separate production cluster
  dns:
    domain: platform.example.com
  github:
    owner: acme
    platformRepo: acme-platform
  admins:
    group: acme-platform-admins   # Entra group of platform admins; created if missing
```

Turn modules on or off, or change their settings, under `spec.modules` (each module's
`docs/README.md` lists its settings):

```yaml
  modules:
    runtime-falco: { enabled: false }
    data-postgres:
      settings: { version: "17" }
```

Then check and preview:

```bash
go run ./cmd/bluepave validate        # the configuration, every module's settings, apps/*.yaml
go run ./cmd/bluepave modules         # the modules available, with what each one does
go run ./cmd/bluepave render          # .bluepave/resolved.yaml and platform-settings.yaml
go run ./cmd/bluepave plan -what-if   # the deployment stacks, in order, and Azure's what-if
go run ./cmd/bluepave preflight       # can this subscription run the profile in this region?
```

`preflight` (which `up` also runs first) checks that every VM size of the profile is offered to
your subscription in the region, in the zones the cluster uses, that the vCPU quota covers the
node pools, and that the PostgreSQL SKU exists there. Free Trial subscriptions often can't use a
size in some regions: pick another region if it reports `NotAvailableForSubscription`.

## 4. Bring it up

```bash
go run ./cmd/bluepave up
```

`up` shows what it will do and asks first. Then it runs these steps, saving progress after each
one (re-running it converges; finished work is a no-op):

1. **accounts:** resource providers, the admins group (you're added), the CI identity that the
   repository's GitHub environments trust, and the repository variables (`BLUEPAVE_*`).
2. **infra:** every module's deployment stack, in order: network, DNS, registry, Key Vault, AKS,
   PostgreSQL, and the identities each add-on uses. This is the long part.
3. **identities:** Entra apps for Argo CD, Grafana and the portal, and their secrets in Key Vault.
4. **github-app:** the platform's GitHub App. A browser opens: confirm the App, then install it on
   your account or organisation (at least on the platform repository). Its key goes to Key Vault.
5. **gitops:** installs Argo CD and hands the platform to it.

The first run stops at **gitops**, because Argo CD reads the configuration from Git:

```bash
git add bluepave.yaml .bluepave apps
git commit -m "Platform configuration" && git push
go run ./cmd/bluepave up -step gitops
```

**Delegate your domain.** `up` prints the Azure DNS name servers for your domain. Set them as NS
records at your registrar (or in the parent zone). Certificates and app URLs work once the
delegation has propagated.

## 5. Check it

```bash
go run ./cmd/bluepave status
```

`status` lists every stack, every Argo CD application (sync and health) and the URLs. It exits
non-zero until everything is healthy; the first sync takes a while. Then sign in with your Entra
account (members of the admins group):

| URL | |
|---|---|
| `https://portal.<env>.<domain>` | The developer portal: catalog, docs, templates |
| `https://argocd.<env>.<domain>` | Argo CD |
| `https://grafana.<env>.<domain>` | Grafana: metrics and traces |

Kargo (promotions between stages) has no public URL yet: `kubectl -n kargo port-forward
svc/kargo-api 8443:443`, then https://localhost:8443 with the admin password from Key Vault
(`kargo-admin-password`; see [delivery-kargo](../modules/delivery-kargo/docs/README.md)).

## 6. Your first app

Either of these opens a pull request that adds a service to the platform repository (code,
chart, onboarding file `apps/<name>.yaml`, workflow):

- **From the portal:** Create → "New Go service" or "New Python service".
- **The example app:** `hack/add-example.sh anvil` adds [anvil](../examples/anvil/), which uses a
  database, a cache and blob storage from the platform APIs. Commit, push a branch and open the
  pull request.

Merge it. Then:
1. The golden path builds, scans and signs the images.
2. App onboarding creates the namespaces `<name>-dev` and `<name>-staging`.
3. Kargo deploys the first images to dev, at `https://<name>.<env>.<domain>`.
4. Promote to staging in Kargo (step 5 shows how to open it).

How apps work on the platform: [apps/README.md](../apps/README.md). The platform APIs (AppStorage,
AppCache, AppDatabase): [modules/self-service](../modules/self-service/docs/README.md).

## 7. Tear it down

```bash
go run ./cmd/bluepave down
```

`down` asks for the platform's name. Then it deletes:
- the GitHub App;
- the Entra apps `up` created;
- every deployment stack, newest first;
- the soft-deleted Key Vaults, where the profile allows purging;
- the repository variables.

It keeps the admins group, which may have existed before the platform. Data that platform APIs
created for apps (storage accounts, databases) lives in the stacks' resource groups and goes with
them.

## When something goes wrong

- **`up` stopped in a step:** fix what it reports and run `bluepave up` again; it continues where
  it left off. `-step <name>` runs one step, `-module` and `-env` narrow `infra`.
- **An Argo CD application is unhealthy:** `bluepave status` names it; open it in Argo CD for the
  failing resource. Each module's `docs/README.md` has a runbook.
- **Certificates pending, URLs don't resolve:** the domain isn't delegated yet (step 4), or
  hasn't propagated. `dig NS <domain>` should show the Azure name servers.
- **Quota or VM size errors:** run `bluepave preflight`. The Free Trial's vCPU quota is per
  region, so pick a region where you have no other VMs, and one that offers the profile's VM size
  to your subscription.
