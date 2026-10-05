# self-service

Platform APIs app teams use to get backing services, by committing a few lines of YAML next to
their app ([ADR-0003](../../../docs/adr/0003-self-service-apis.md)). The platform decides the
guardrails; the app gets a keyless workload identity with access to exactly that resource.

| API | Gives the app | Status |
|---|---|---|
| `AppStorage` | A storage account and blob container, Entra ID only | Available |
| `AppCache` | A cache: Valkey in the namespace (`data.cache: inCluster`) | Available; Azure Managed Redis (`azureManagedRedis`) in a later version |
| `AppDatabase` | A database and an Entra role on the platform's PostgreSQL server (`data-postgres`) | Available when `data-postgres` is on |

## AppStorage

```yaml
apiVersion: platform.bluepave.dev/v1alpha1
kind: AppStorage
metadata: { name: archive, namespace: anvil-dev }
spec:
  serviceAccountName: anvil-api   # the app's service account, trusted by the new identity
  container: data                 # default
  access: readWrite               # or readOnly
```

[kro](https://kro.run) expands the object into [Azure Service Operator](https://azure.github.io/azure-service-operator/)
resources, and ASO creates them in `rg-<prefix>-<env>-<region>-apps`:
- a managed identity, federated to the app's service account;
- a StorageV2 account with no keys (`allowSharedKeyAccess: false`), no public blobs and TLS 1.2;
- a blob container, with 7-day soft delete;
- Storage Blob Data Contributor (or Reader) for that identity, on that account only;
- a ConfigMap `<name>-storage` with `AZURE_STORAGE_ACCOUNT`, `AZURE_STORAGE_BLOB_ENDPOINT`,
  `AZURE_STORAGE_CONTAINER` and `AZURE_STORAGE_CLIENT_ID`.

**Deleting an `AppStorage` keeps the data.** The identity, its federation and the role assignment
go, but the account and container stay in Azure (ASO `detach-on-delete`). Delete them in Azure
deliberately, or re-create the `AppStorage` to reattach.

**Only where the profile allows public network access** (`trial`, `standard`): access is by
Entra ID only, but the account endpoint is public. Private endpoints come with a later version,
so `production` doesn't offer AppStorage yet. `settings.apis.appStorage` overrides this.

## AppCache

```yaml
apiVersion: platform.bluepave.dev/v1alpha1
kind: AppCache
metadata: { name: sessions, namespace: anvil-dev }
spec:
  size: small        # small (64 MB), medium (256 MB) or large (1 GB)
```

With the profile's `data.cache: inCluster` (`trial`, `standard`), kro creates in the app's
namespace:
- a Valkey Deployment and Service, `<name>-cache`, evicting the least recently used keys when
  full. It's a cache: nothing is persisted, and a restart starts empty.
- NetworkPolicies: only the namespace's own pods reach it (no password; the namespace is the
  boundary), they may reach it despite the app's default-deny egress, and Valkey itself has no
  egress;
- a ConfigMap `<name>-cache` with `CACHE_HOST`, `CACHE_PORT`, `CACHE_TLS` (`false`),
  `CACHE_AUTH` (`none`) and `CACHE_URL`. Azure Managed Redis will fill the same keys (with TLS and
  `CACHE_AUTH: entra`), so apps read them rather than assume a backing.

The Valkey image is pinned by digest in `module.yaml` (`spec.appImages`), which is how the
admission policy (policy-kyverno) allows it in app namespaces; CI checks the resource graph uses
exactly that image.

## AppDatabase

```yaml
apiVersion: platform.bluepave.dev/v1alpha1
kind: AppDatabase
metadata: { name: orders, namespace: anvil-dev }
spec:
  serviceAccountName: anvil-api   # the app's service account, trusted by the new identity
```

On the platform's PostgreSQL server (`data-postgres`, Entra ID sign-in only, private endpoint):
- a managed identity, federated to the app's service account;
- a database `<namespace>-<name>` and a role of the same name mapped to that identity, with all
  privileges on the database and its `public` schema. A Job in `appdatabase-system` creates them
  as the server's AppDatabase admin (`id-<prefix>-<env>-<region>-pgadmin`, an Entra
  administrator of the server), because ASO can't create Entra roles inside PostgreSQL;
- a NetworkPolicy letting the namespace's pods reach the server's private endpoint on 5432;
- a ConfigMap `<name>-database` with `PGHOST`, `PGPORT`, `PGDATABASE`, `PGUSER`, `PGSSLMODE`
  (`require`), `DATABASE_AUTH` (`entra`) and `DATABASE_CLIENT_ID`.

The app signs in as `PGUSER` with an Entra access token as the password: a token for
`DATABASE_CLIENT_ID` and the scope `https://ossrdbms-aad.database.windows.net/.default`
(Workload Identity: label the pod `azure.workload.identity/use: "true"`). Tokens last about an
hour, so take a fresh one for each new connection.

**Deleting an `AppDatabase` keeps the database and its role.** The identity, the job and the
policy go. Re-creating it reattaches: the job points the role at the new identity. Drop the
database deliberately, as a server administrator.

`<namespace>-<name>` must fit PostgreSQL's 63-character limit for names.

## How it's locked down

- **ASO** signs in with Workload Identity (`id-<prefix>-<env>-<region>-aso`). On the apps
  resource group only, it holds:
  - Managed Identity Contributor and Storage Account Contributor;
  - Role Based Access Control Administrator, with a condition: it may grant or remove only the
    data roles the APIs use, and only to service principals. It can't make anyone Owner or grant
    itself more.

  Creating that assignment needs Owner or User Access Administrator on the subscription.
- **kro** runs with aggregated RBAC: it may manage only the platform API kinds and the kinds
  they're built from: ASO's, plus Deployments, Services, NetworkPolicies and ConfigMaps for
  AppCache. That role is cluster-wide; the resource graphs create objects only in the namespace
  of the object they expand.
- **ASO installs only the CRD groups** the APIs use (`crdPattern`), not several hundred.
- **App namespaces can't create ASO kinds directly**, and a quota limits each API per namespace
  (two of each API).
  App onboarding enforces both (its Argo CD project and a ResourceQuota).

## Settings

```yaml
# bluepave.yaml
spec:
  modules:
    self-service:
      settings:
        apis:
          appStorage: true
          appCache: true
          appDatabase: true          # default: on when data-postgres is
```

**Outputs:** `clientId`, `appsResourceGroupId`, `oidcIssuerUrl`, and with `data-postgres`
`postgresAdminClientId`, `postgresAdminName`. The module is deployed after `data-postgres`
when that's on (`spec.after`).

## Runbook

- **Status of a request:** `kubectl get appstorage -n <ns>`, then
  `kubectl get storageaccount,userassignedidentity,roleassignment -n <ns>`. ASO puts Azure's
  errors in each object's `Ready` condition.
- **Role assignment refused (403):** the RBAC Administrator condition only allows the APIs' data
  roles. A new API that grants another role needs it added to the condition (`infra/main.bicep`).
- **The cluster was re-created:** its OIDC issuer changed. Re-run `bluepave up` so the identities'
  federations and `platform-settings` follow.
