# portal

The platform side of the developer portal. The portal itself (Backstage, `portal/` in the platform
repository) runs as an app: built and signed by the golden path, onboarded by `apps/portal.yaml`
and delivered by Kargo. This module provides what an app can't provide for itself, in the
portal's namespace (`portal-<environment>`):

| Resource | Why the platform provides it |
|---|---|
| `portal-config` ConfigMap (`app-config.platform.yaml`) | The platform's settings, rendered from the module context: URLs, the GitHub App integration (`allowedInstallationOwners`), catalog locations, the cluster, and home page links to the platform UIs that are enabled |
| `portal-settings` ConfigMap | The Entra app's client ID and tenant (IDs only) |
| `portal-secrets` ExternalSecret | The Entra client secret and the GitHub App's ID, client ID and key, from Key Vault. Apps may not create ExternalSecrets |
| `portal-reader` ClusterRole | Read-only workloads for the Kubernetes tab, never Secrets or ConfigMaps. Apps may not create cluster-scoped RBAC |
| HTTPRoute | `https://portal.<env>.<domain>` on the Gateway's wildcard listener |
| NetworkPolicy | Ingress only from the Gateway. Egress to DNS, the API server subnet and public HTTPS (GitHub, Entra ID) |

`bluepave up` creates the portal's Entra app (`discovered.environments.<env>.portal.appClientId`)
and stores its client secret in Key Vault (`portal-entra-client-secret`).

## Settings

```yaml
# bluepave.yaml
spec:
  modules:
    portal:
      settings:
        environment: dev                  # the cluster that runs the portal
        catalogLocations:                 # extra catalog files, e.g. app repositories
          - https://github.com/acme/greeter/blob/main/catalog-info.yaml
```

To run without a portal, disable the module and delete `apps/portal.yaml`.

## Runbook

- **"Failed to sign in as a user":** the user has no catalog entry. Add a `User` with their Entra
  object ID to `catalog/org.yaml`.
- **Catalog empty or errors:** check the GitHub App's installation (contents read) and the
  `portal-secrets` sync (`kubectl -n portal-dev get externalsecret`).
- **Kubernetes tab shows nothing:** check the NetworkPolicy's API server range
  (`discovered...network.apiServerSubnetPrefix`) and the `portal-reader` binding.
