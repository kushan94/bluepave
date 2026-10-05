# Portal (Backstage)

The platform's developer portal: a catalog of the platform and its apps, TechDocs, a live
Kubernetes view per app, and templates for new services.

## How it runs

- **Built by the golden path** like any app (`.github/workflows/portal.yml`): the image
  `portal-web`, scanned and signed. It's delivered by the generated Kargo pipeline
  (`apps/portal.yaml`) to `https://portal.<env>.<domain>`.
- **One generic image.** Nothing platform-specific is baked in. The `portal` module
  (`modules/portal`) renders `app-config.platform.yaml` (URLs, the GitHub App integration, catalog
  locations, the cluster, home page links) and mounts it at `/app/config`. It also provides the
  route, the network policy, read-only RBAC for the Kubernetes tab, and the secrets from Key Vault.
- **Config files, later ones win** (maps merge; arrays and scalars replace):
  1. `app-config.yaml`: local development defaults;
  2. `app-config.production.yaml`: Entra ID sign-in, SQLite on scratch storage, TechDocs, the
     Kubernetes kinds;
  3. `/app/config/app-config.platform.yaml`: the platform's settings.
- **Sign-in:** Microsoft Entra ID only. A user is matched to a catalog `User` by their Entra object
  ID, so add people to `catalog/org.yaml`.

## Run it locally

You need Node.js 22 or 24 with Corepack (Yarn 4 comes from `.yarn/releases`). For TechDocs you
also need mkdocs, and for the Kubernetes tab `kubectl` with access to a cluster.

```bash
# once
pip install mkdocs-techdocs-core          # TechDocs (or in a virtualenv on PATH)
cd portal && yarn install

# each time, in two terminals
kubectl proxy --port=8001                 # Kubernetes tab (uses your kubeconfig)
export GITHUB_TOKEN=$(gh auth token)      # templates open pull requests as you
cd portal && yarn start                   # http://localhost:3000, sign in as guest
```

Locally the catalog is read from your checkout (`catalog-info.yaml` at the repository root).

## Adding plugins

The portal is ordinary Backstage source in your platform repository, so you can add plugins.
Add the plugin to `packages/app` or `packages/backend`, commit, and the golden path builds,
scans and signs the new image.

## Notes

- **Yarn resolution:** `package.json` pins `@yarnpkg/core` to 4.9.1. Version 4.9.2 was published
  with a dependency on a patch file that only exists in Yarn's own repository, so `yarn install`
  fails with it.
- **State:** SQLite and generated TechDocs live on the pod's scratch volume and are rebuilt on
  restart. Scaffolder task history doesn't survive a restart.
