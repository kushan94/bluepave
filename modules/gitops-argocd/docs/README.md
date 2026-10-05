# gitops-argocd

Argo CD, the platform's GitOps engine. It manages every enabled module from Git, and itself.

- **Self-managing:** `bluepave up` installs Argo CD once with Helm (release `argo-cd`, namespace
  `argocd`), then applies the root Application. This module's `argo-cd` Application has the same
  release name and namespace, so Argo CD adopts the install and upgrades itself from Git after
  that.
- **Microsoft Entra ID sign-in only:** local accounts and the admin password are off. Once
  `bluepave up` has created the Entra app (`discovered.environments.<env>.gitops-argocd.clientId`),
  argocd-server signs users in with Workload Identity, so no client secret exists. Before that,
  use `argocd --core`, which goes through your Entra-authenticated kubeconfig.
- **Access:** the platform admins group is `role:admin`. Nobody else has access by default.
- **Git access:** with `secrets-external` enabled, the platform's GitHub App is a credential
  template for every `https://github.com/<owner>/...` repository. Argo CD mints short-lived
  installation tokens from the App's key in Key Vault, so no personal token or deploy key exists.
- **AKS:** ignores the namespace selectors that AKS adds to every admission webhook, which
  would otherwise show as OutOfSync forever.
- **Served at** `https://argocd.<env>.<domain>` once the `edge-gateway` module is enabled. TLS
  ends at the Gateway, so argocd-server runs plain HTTP behind it.
- **Scheduling:** on profiles with Spot pools (`trial`), Argo CD may run on Spot nodes. An
  eviction only delays reconciliation.

## Settings

```yaml
# bluepave.yaml
spec:
  modules:
    gitops-argocd:
      settings:
        values:            # extra argo-cd chart values, merged last (escape hatch)
          controller:
            resources: { limits: { memory: 2Gi } }
```

The upstream chart version is pinned in `gitops/values.yaml` (`upstream.version`) and bumped by
module releases.

## Runbook

- **UI before the Gateway exists:** run `kubectl -n argocd port-forward svc/argo-cd-argocd-server 8080:443`.
- **CLI without SSO:** run `argocd app list --core`.
- **Controller OOMKilled** with many apps or large CRDs: raise `controller.resources` through
  `settings.values`.
