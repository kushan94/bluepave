# observability

Metrics, traces and dashboards for the platform and its apps.

| Signal | Path |
|---|---|
| Metrics | The AKS managed Prometheus agent (wired by the `aks` module) scrapes the cluster, and every app stage namespace (`<app>-<stage>`) through `prometheus.io/*` pod annotations. Metrics go to the environment's Azure Monitor workspace (`monitoring`) |
| Traces | Apps send OTLP to `otel-collector.observability:4317`. The collector forwards to Tempo, which keeps 24 hours on local disk |
| Dashboards | Grafana at `https://grafana.<env>.<domain>` (with `edge-gateway`), over managed Prometheus and Tempo. Dashboards are ConfigMaps labelled `grafana_dashboard` in `observability` (as code) |

- **Grafana sign-in:** Microsoft Entra ID only (no login form, no local users). Only the platform
  admins group is admitted, and they're Grafana admins. Grafana uses Workload Identity instead
  of a client secret; `bluepave up` creates its Entra app (`observability.appClientId`).
- **Metrics access:** Grafana queries managed Prometheus as
  `id-<prefix>-<env>-<region>-grafana`, which holds Monitoring Data Reader on the workspace
  (read-only).
- **Apps never talk to a tracing backend:** the collector is the only endpoint, so Tempo can be
  replaced without redeploying apps.
- **Tempo trades durability for cost:** traces don't survive a Tempo restart. That's fine for
  debugging; use a storage-backed Tempo or a managed backend if you need retention.
- SLO alert rules belong to each app (app onboarding), not to this module.

## Upgrades

- **Charts come from `grafana-community`** (grafana 13.2.7 with Grafana 13.2, tempo 3.1.0 with
  Tempo 3.1). Grafana Labs moved the charts there on 2026-01-30 and froze the copies at
  `grafana.github.io/helm-charts`.
- **Moving from Grafana 12 / Tempo 2.9** (module 0.1.x before this change) needs nothing: Grafana
  keeps no state (users from Entra ID, dashboards from Git), and Tempo's traces are kept only
  until a restart anyway. Tempo 3 runs as a single process with local storage, like before;
  retention moved into its backend scheduler, which the chart configures.

**Outputs:** `clientId`, `monitorWorkspaceQueryEndpoint`.

## Runbook

- **Grafana says "login failed":** the user isn't in the admins group, or the Entra app's
  federated credential doesn't match the cluster's OIDC issuer (re-run `bluepave up`).
- **"No data" from Prometheus:** check that the identity has Monitoring Data Reader on the
  workspace, and that the data source URL is the workspace's query endpoint.
- **An app's metrics are missing:** its namespace must match `<app>-(dev|staging|prod)` and its
  pods must carry `prometheus.io/scrape: "true"` and `prometheus.io/port`.
