# self-service

Platform APIs app teams use to get Azure resources, by committing a few lines of YAML next to
their app ([ADR-0003](../../../docs/adr/0003-self-service-apis.md)). The platform decides the
guardrails; the app gets a keyless workload identity with access to exactly that resource.

| API | Gives the app | Status |
|---|---|---|
| `AppStorage` | A storage account and blob container, Entra ID only | Available |
| `AppCache` | Valkey in the namespace or Azure Managed Redis, per profile | Next version |
| `AppDatabase` | A database and role on the shared PostgreSQL server | Next version |

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

## How it's locked down

- **ASO** signs in with Workload Identity (`id-<prefix>-<env>-<region>-aso`). On the apps
  resource group only, it holds:
  - Managed Identity Contributor and Storage Account Contributor;
  - Role Based Access Control Administrator, with a condition: it may grant or remove only the
    data roles the APIs use, and only to service principals. It can't make anyone Owner or grant
    itself more.

  Creating that assignment needs Owner or User Access Administrator on the subscription.
- **kro** runs with aggregated RBAC: it may manage only the platform API kinds and the ASO kinds
  they're built from.
- **ASO installs only the CRD groups** the APIs use (`crdPattern`), not several hundred.
- **App namespaces can't create ASO kinds directly**, and a quota limits each API per namespace.
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
```

**Outputs:** `clientId`, `appsResourceGroupId`, `oidcIssuerUrl`.

## Runbook

- **Status of a request:** `kubectl get appstorage -n <ns>`, then
  `kubectl get storageaccount,userassignedidentity,roleassignment -n <ns>`. ASO puts Azure's
  errors in each object's `Ready` condition.
- **Role assignment refused (403):** the RBAC Administrator condition only allows the APIs' data
  roles. A new API that grants another role needs it added to the condition (`infra/main.bicep`).
- **The cluster was re-created:** its OIDC issuer changed. Re-run `bluepave up` so the identities'
  federations and `platform-settings` follow.
