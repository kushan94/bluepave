# secrets-external

[External Secrets Operator](https://external-secrets.io) copies secrets from the environment's Key
Vault into Kubernetes Secrets. It's how platform modules get credentials: the GitHub App key for
Argo CD and Kargo, Kargo's admin password hash, and the portal's Entra client secret.

- **No stored credentials:** the operator signs in to Key Vault with Workload Identity
  (`id-<prefix>-<env>-<region>-external-secrets`, federated to the `external-secrets` service
  account). It holds Key Vault Secrets User on that vault only, so it can read secret values but
  can't list keys or manage the vault.
- **One store:** the `ClusterSecretStore` `azure-keyvault`, rendered once `bluepave up` has
  recorded the vault's URI (`discovered.environments.<env>.keyvault.keyVaultUri`).
- **Platform only:** app projects may not create `ExternalSecret`s (app onboarding). Apps reach
  Azure with their own workload identities instead.

Other modules integrate when this one is enabled: they render `ExternalSecret`s that reference
`azure-keyvault`.

**Outputs:** `clientId` (the identity's client ID, recorded as
`discovered.environments.<env>.secrets-external.clientId`).

## Runbook

- **Is a secret syncing?** Run `kubectl get externalsecret -A`. `SecretSynced=False` gives the
  reason in its message.
- **Forbidden from Key Vault:** check that the role assignment exists and that the secret name
  matches. Role assignments can take a few minutes to apply.
