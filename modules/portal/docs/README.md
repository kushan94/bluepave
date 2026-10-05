# portal

The platform side of the developer portal. The portal itself (Backstage, `portal/` in the platform
repository) runs as an app: built and signed by the golden path, onboarded by `apps/portal.yaml`
and delivered by Kargo. This module provides what an app can't provide for itself, in the
portal's namespace (`portal-<environment>`):

| Resource | Why the platform provides it |
|---|---|
| `portal-config` ConfigMap (`app-config.platform.yaml`) | The platform's settings, rendered from the module context: URLs, the GitHub App integration (`allowedInstallationOwners`), catalog locations and rules, the cluster, and home page links to the platform UIs that are enabled |
| `portal-config` ConfigMap (`platform-settings.yaml`) | The `platform-settings` catalog entity: the platform's GitHub owner, repository, revision, environment and domain, which the templates read |
| `portal-settings` ConfigMap | The Entra app's client ID and tenant (IDs only) |
| `portal-secrets` ExternalSecret | The Entra client secret and the GitHub App's ID, client ID and key, from Key Vault. Apps may not create ExternalSecrets |
| `portal-reader` ClusterRole | Read-only workloads for the Kubernetes tab, never Secrets or ConfigMaps. Apps may not create cluster-scoped RBAC |
| HTTPRoute | `https://portal.<env>.<domain>` on the Gateway's wildcard listener |
| NetworkPolicy | Ingress only from the Gateway. Egress to DNS, the API server subnet and public HTTPS (GitHub, Entra ID) |

`bluepave up` creates the portal's Entra app (`discovered.environments.<env>.portal.appClientId`)
and stores its client secret in Key Vault (`portal-entra-client-secret`).

## Templates

`templates/` has the portal's software templates, **New Go service** and **New Python service**.
Each one opens a single pull request to the platform repository that adds:
- `services/<name>/`: the service, its Dockerfile, Helm chart (`deploy/chart`, with
  `deploy/values-dev.yaml` and `values-staging.yaml`) and catalog entry;
- `apps/<name>.yaml`: its onboarding, with stages `dev` and `staging` on the portal's cluster;
- `.github/workflows/<name>.yml`: the golden path for it, publishing with the platform's CI
  identity.

After the merge the golden path builds and signs `<name>-api` (Go) or `<name>-web` (Python), app
onboarding creates the namespaces, and Kargo deploys the first image to dev. Optional: a public
route (`<name>.<env>.<domain>`, `<name>-staging.<env>.<domain>`) and blob storage (`AppStorage`).

The templates have nothing platform-specific in them: they read the `platform-settings` entity.
`templates/common/` is shared by both (chart, onboarding, workflow, catalog entry); each template's
`skeleton/` has only its code. `hack/test-templates.sh` renders both the way Backstage does and runs
the platform's checks on the result.

**Services live in the platform repository.** A GitHub App can't create repositories for a
personal account, and an app in its own repository needs its own CI identity, which app
onboarding doesn't create yet.

**Who may add templates:** the catalog accepts `Template`, `Group` and `User` entities only from
the platform repository (at the GitOps revision), so they go through review like everything else.

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
- **A template fails at "Read the platform's settings":** the `platform-settings` entity is
  missing. It's read from `/app/config/platform-settings.yaml` (the `portal-config` ConfigMap);
  check the ConfigMap and the catalog's errors. Running the portal locally, there's no such
  entity, so templates can't run there.
- **A template's pull request fails:** the GitHub App needs `contents`, `pull_requests` and
  `workflows` write on the platform repository (`bluepave up` creates it so).
- **Kubernetes tab shows nothing:** check the NetworkPolicy's API server range
  (`discovered...network.apiServerSubnetPrefix`) and the `portal-reader` binding.
