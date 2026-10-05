# delivery-kargo

[Kargo](https://kargo.io) promotes each app's releases through its stages (for example dev, then
staging). Each promotion is a commit to Git that Argo CD then syncs.

- **Freight from the registry:** Warehouses watch the environment's registry for new images,
  signed by the golden path. The controller reads image metadata as a workload identity
  (`id-<prefix>-<env>-<region>-kargo`, federated to `kargo:kargo-controller`). With repository
  permissions that identity has Repository Reader, otherwise AcrPull. No registry credentials
  exist.
- **Commits as the platform's GitHub App:** promotion steps push to each app's repository with
  short-lived installation tokens. The App's key is in Key Vault, synced into
  `kargo-shared-resources` by External Secrets, so it's available to every Kargo project.
- **Pipelines come from app onboarding:** an app's Project, Warehouse and Stages are generated
  from its onboarding file. This module installs only Kargo itself.
- **Admin account:** its bcrypt hash and token-signing key come from Key Vault, and Helm
  generates no secrets. `bluepave up` creates the password and keeps it in Key Vault as
  `kargo-admin-password`. Microsoft Entra ID sign-in (Kargo supports OIDC with PKCE) comes in a
  later version, once it's proven in the reference instance.
- **cert-manager** issues the certificates for Kargo's webhooks and API server, so this module
  requires `certificates`.
- **No inbound webhooks:** Warehouses poll the registry, which keeps one more component off
  small clusters.

**Outputs:** `clientId`.

**Key Vault secrets it reads:** `kargo-admin-password-hash`, `kargo-token-signing-key`,
`github-app-id`, `github-app-installation-id`, `github-app-private-key`.

## Runbook

- **UI:** run `kubectl -n kargo port-forward svc/kargo-api 8443:443`, open
  https://localhost:8443, and get the password with
  `az keyvault secret show --vault-name <vault> -n kargo-admin-password --query value -o tsv`.
- **Warehouse finds no Freight after a registry permission change:** Kargo caches registry
  tokens. Restart the controller with `kubectl -n kargo rollout restart deploy/kargo-controller`.
- **Promotion can't push:** check that the `github-app` secret in `kargo-shared-resources` is
  synced, and that the App is installed on the app's repository with contents write.
