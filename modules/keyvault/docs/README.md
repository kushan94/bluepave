# keyvault

Key Vault, one per environment (`kv-<prefix>-<env>-<region>-<hash>`), for platform secrets: Git
deploy keys, the platform GitHub App's private key, the portal's Entra client secret. Access is
Entra ID RBAC only (no access policies). In the cluster, External Secrets reads it with Workload
Identity.

| Profile setting | Effect |
|---|---|
| `keyVault.purgeProtection` | Can't be turned off once on, and blocks re-creating a vault with the same name during retention (off in `trial`) |
| `keyVault.softDeleteDays` | Recovery window for deleted secrets |
| `network.allowPublicNetworkAccess`, `network.privateEndpoints` | Public endpoint, or private only through the spoke |

The platform admins group gets Key Vault Administrator once `bluepave up` has discovered it.

**Outputs:** `keyVaultName`, `keyVaultUri`.
