# rollouts

[Argo Rollouts](https://argoproj.github.io/rollouts/) replaces an app's Deployment with a
`Rollout`. A Rollout releases new versions as a canary or blue-green, checks them with an
`AnalysisTemplate` (for example a Prometheus query for the error rate), and rolls back
automatically when the check fails.

- **Used by the golden path:** the app templates ship a canary Rollout and an analysis on the
  app's SLIs. Kargo's verification step can run the same analysis after each promotion.
- **CRDs are kept** when the chart is removed, so uninstalling can't delete every app's
  Rollouts, and with them their pods.
- **Replicas:** two controllers with leader election where the profile is highly available (an
  AKS tier with an uptime SLA), one on `trial`.
- **No dashboard:** use `kubectl argo rollouts` or Argo CD's Rollout view.

## Runbook

- **Watch a release:** `kubectl argo rollouts get rollout <app> -n <namespace> --watch`.
- **Stuck at a pause step:** `kubectl argo rollouts promote <app> -n <namespace>`.
- **Roll back now:** `kubectl argo rollouts abort <app> -n <namespace>`, then fix the image in Git.
  Otherwise Argo CD re-applies it.
