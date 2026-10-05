// Lets the kubelet pull images from the environment's registry, with no image pull secrets. With
// repository permissions (ABAC) the registry ignores AcrPull, so the kubelet gets Container Registry
// Repository Reader (all repositories) instead.

@description('Container registry name.')
param registryName string

@description('Object ID of the identity that pulls.')
param principalId string

@description('Whether the registry uses repository permissions (profile registry.repositoryPermissions).')
param repositoryPermissions bool

var roleId = subscriptionResourceId(
  'Microsoft.Authorization/roleDefinitions',
  repositoryPermissions
    ? 'b93aa761-3e63-49ed-ac28-beffa264f7ac' // Container Registry Repository Reader
    : '7f951dda-4ed3-4680-a7ca-43fe172d538d' // AcrPull
)

resource registry 'Microsoft.ContainerRegistry/registries@2025-04-01' existing = {
  name: registryName
}

resource pull 'Microsoft.Authorization/roleAssignments@2022-04-01' = {
  name: guid(registry.id, principalId, roleId)
  scope: registry
  properties: {
    principalId: principalId
    principalType: 'ServicePrincipal'
    roleDefinitionId: roleId
  }
}
