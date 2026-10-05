# ADR-0003: Apps get Azure resources through platform APIs, implemented with ASO and kro

- **Status:** Accepted
- **Date:** 2026-10-05

## Context

Apps need their own backing services: blob storage, a cache, a database. There are three ways
to provide them:
1. **The platform creates them centrally**, e.g. a `data-redis` module per environment. App
   teams wait on platform changes, and every app shares one instance.
2. **Apps write raw Azure resources** (ASO or Crossplane managed resources) in their namespaces.
   Teams get every Azure setting, and admission policy has to police each field.
3. **Apps request small platform APIs** (`AppStorage`, `AppCache`, `AppDatabase`), and the
   platform decides how each request is met.

The reference instance (Skyforge) already runs one API of the third kind: `AppStorage`, a
storage account with a workload identity, built with a kro ResourceGraphDefinition over Azure
Service Operator (ASO) v2.

For the engine there are two candidates: ASO + kro, or Crossplane (composition functions plus
the Azure providers).

## Decisions

### 1. Apps request platform APIs, never raw Azure resources

- APIs live in the group `platform.bluepave.dev`, are namespaced, and are small: a name, a size
  enum, a few options.
- Guardrails are set by the platform, not the app: SKU (from the size, mapped by the profile),
  private endpoint, Entra ID-only auth, tags, diagnostic settings, and the app's workload
  identity with the data role it needs.
- App namespaces may not create ASO kinds directly; only the platform's resource graphs create
  them. App onboarding enforces this with the app's Argo CD project, which allows only the
  platform's API kinds, not ASO's.
- Each API publishes connection details in a ConfigMap. They aren't secrets, because access is by
  workload identity.

### 2. The profile chooses what backs an API

The same manifest works in every environment. For example, `AppCache` is:
- **Valkey in the app's namespace** with `data.cache: inCluster` (trial, standard);
- **Azure Managed Redis** with Entra ID auth and a private endpoint with
  `data.cache: azureManagedRedis` (production).

There's no platform-wide Redis module.

### 3. The first APIs

| API | Backed by |
|---|---|
| `AppStorage` | A storage account and blob container (ported from the reference instance) |
| `AppCache` | Valkey or Azure Managed Redis, per profile |
| `AppDatabase` | A database and an Entra role for the app's identity on the shared `data-postgres` server. A job creates the role, because ASO can't create Entra principals inside PostgreSQL. |

More APIs are added only when an app needs one.

### 4. Engine: ASO + kro, behind the API

**Why:**
- **ASO is Azure-native and supported by Microsoft.** It's generated from the Azure API
  definitions, so its fields match the Azure documentation, new features arrive quickly, and
  only the resource types the platform selects are installed. That matters on the `trial`
  profile's small nodes.
- **kro is a thin layer:** CEL expressions over resources, with no new runtime model.
- **It's proven:** the reference instance runs `AppStorage` this way, with Workload Identity
  and roles limited to one resource group.

**Crossplane** has more mature composition (functions with real logic), a larger ecosystem
(GitHub, Helm, SQL roles) and multi-cloud reach. It wasn't chosen because of:
- the footprint of its Azure providers;
- the Terraform-derived translation layer in its Azure provider;
- licensing changes around the official provider packages.

**The API is the contract.** The engine is an implementation detail of the `self-service`
module, so a Crossplane-backed implementation can replace it, or ship as an alternative module,
without app changes.

**Revisit when:**
- adopters need more than Azure;
- a composition outgrows CEL;
- kro stalls or breaks its API before v1;
- most adopters already run Crossplane.

### 5. Guardrails

- **Sizes are enums,** and each profile maps them to SKUs.
- **A per-namespace quota limits each API** (e.g. at most two caches). It's a ResourceQuota on
  object counts (`count/appcaches.platform.bluepave.dev`), created by app onboarding with each
  app namespace; no admission policy is needed.
- **Data outlives its Kubernetes object.** Data-bearing Azure resources use ASO's `detach`
  reconcile policy on deletion, so deleting the object (or the app) keeps the data in Azure.
  Removing it is a separate, deliberate step.
- **ASO's identity** may only create resources in the apps resource group. It can grant only the
  specific data roles the APIs use.

### 6. Every API has conformance tests

Each API is tested in the end-to-end run:
- create it;
- connect from a pod with the app's workload identity;
- delete it, and check the data is kept;
- check the quota is enforced.

Any engine behind the API must pass the same tests.

## Consequences

- One module, `self-service`, replaces `self-service-appstorage`. Its settings choose which APIs
  are on. It requires `aks` and `gitops-argocd` (and `data-postgres` once `AppDatabase` lands).
  The per-namespace guardrails (quota, no raw ASO kinds) live in app onboarding, which creates
  the namespaces.
- Private endpoints for API resources come with a later module version. Until then, profiles
  without public network access (`production`) don't offer AppStorage, rather than creating
  public accounts.
- The profile's `data.cache` now selects the backing of `AppCache`, not a platform service.
- Apps' hand-written in-cluster services (the reference app's Valkey) are replaced by
  `AppCache`.
- kro's API may change before v1. The API boundary limits the impact to the module.
