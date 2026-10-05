# monitoring

Per-environment monitoring foundation, used by `aks` (control-plane logs, managed Prometheus) and
`observability`.

| Resource | Settings (profile) |
|---|---|
| `rg-<prefix>-<env>-<region>-mgmt` | |
| Log Analytics `log-<prefix>-<env>-<region>` | `logs.retentionDays`, `logs.dailyQuotaGb` (-1 means no cap) |
| Azure Monitor workspace `amw-<prefix>-<env>-<region>` | Managed Prometheus metrics |

**Outputs:** `logAnalyticsWorkspaceId`, `monitorWorkspaceId`.
