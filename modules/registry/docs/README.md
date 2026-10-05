# registry

Azure Container Registry, one per environment (`cr<prefix><env><region><hash>`). Entra ID is the
only way to authenticate: no admin user and no anonymous pull.

| Profile setting | Effect |
|---|---|
| `registry.sku` | Standard, or Premium (needed for private endpoints, zone redundancy and retention) |
| `registry.repositoryPermissions` | Role assignments by repository (ABAC), so each app's CI identity is limited to its `<app>-*` images. ACR then ignores AcrPull/AcrPush, and this module grants the repository roles instead. |
| `registry.armTokenAuth` | Accept ARM-audience Entra tokens at the token exchange. Kyverno's Azure credential provider needs it to verify signatures. |
| `network.allowPublicNetworkAccess`, `network.privateEndpoints` | Public endpoint, or private only through the spoke |

**Who can push:** the platform admins group and the CI identity get Repository Writer, or AcrPush
in classic mode. Both IDs are discovered by `bluepave up`. Apps' own CI identities come from the
app onboarding.

Untagged manifests are deleted after 7 days.

**Outputs:** `containerRegistryName`, `containerRegistryLoginServer`.
