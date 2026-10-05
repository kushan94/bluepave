# data-postgres

One PostgreSQL Flexible Server per environment (`psql-<prefix>-<env>-<region>-<hash>`), in
`rg-<prefix>-<env>-<region>-data`.

- **No passwords:** Entra ID authentication only. The platform admins group is the Entra admin
  once `bluepave up` has discovered it. Apps sign in with their workload identities.
- **Apps' databases** come from the `AppDatabase` API (`self-service`), which adds its own Entra
  administrator to this server (`id-<prefix>-<env>-<region>-pgadmin`) to create them.
- **Private only:** public network access is off in every profile. The server is reached through
  a private endpoint in the spoke's `snet-private-endpoints`, resolved by the
  `privatelink.postgres.database.azure.com` zone that the `network` module links to both VNets.
- **Logs** go to the environment's Log Analytics workspace.

| Profile setting | Effect |
|---|---|
| `data.postgres.skuName`, `tier` | Compute: Burstable B1ms on `trial`, General Purpose otherwise |
| `data.postgres.storageSizeGB` | Initial storage (it grows automatically) |
| `data.postgres.highAvailability` | `ZoneRedundant`: a standby in another zone (primary zone 1, standby zone 2) |
| `data.postgres.backupRetentionDays`, `geoRedundantBackup` | Point-in-time restore window, backups copied to the paired region |

## Settings

```yaml
# bluepave.yaml
spec:
  modules:
    data-postgres:
      settings:
        version: "18"                 # default
        databases: [platform_reports]  # the platform's own; apps use AppDatabase
        threatProtection: true        # default: off on trial, on otherwise (billed per server)
        maintenanceWindow:            # default: Sunday 02:00 UTC
          dayOfWeek: 0                # 0 = Sunday
          startHour: 2
          startMinute: 0
```

**Outputs:** `postgresServerName`, `postgresFqdn`.

## Runbook

- **Connect as an admin** from inside the VNet (or from a pod), with a member of the admins
  group: `PGPASSWORD=$(az account get-access-token --resource-type oss-rdbms --query accessToken -o tsv) psql "host=<fqdn> dbname=postgres user=<admins group name> sslmode=require"`.
- **Give an app access:** use `AppDatabase`. By hand: create a role for its workload identity
  with `select * from pgaadauth_create_principal_with_oid('<role>', '<principal id>', 'service', false, false);`,
  then grant it rights on the database.
- **Major version upgrades** aren't automatic. Change `version` only after an in-place
  upgrade with `az postgres flexible-server upgrade`.
