// The identity Kyverno reads image signatures from the registry as: its admission controller (at
// pod creation) and its reports controller (background scans). Federated to both service accounts,
// with pull rights on the registry only.
targetScope = 'subscription'

import {
  envName
  federatedCredential
  location
  profile
  resourceNames
  tagsFor
  workloadIdentityName
} from '../../../lib/bicep/platform.bicep'

@description('Environment to deploy.')
param environmentName envName

var names = resourceNames(environmentName, subscription().subscriptionId)
var tags = tagsFor(environmentName)
var issuer = cluster.properties.oidcIssuerProfile.issuerURL

resource cluster 'Microsoft.ContainerService/managedClusters@2026-05-01' existing = {
  name: names.aksCluster
  scope: resourceGroup(names.rgAks)
}

module identity 'br/public:avm/res/managed-identity/user-assigned-identity:0.6.0' = {
  scope: resourceGroup(names.rgAks)
  params: {
    name: workloadIdentityName(environmentName, 'kyverno')
    location: location
    tags: tags
    federatedIdentityCredentials: [
      federatedCredential(issuer, 'kyverno', 'kyverno-admission-controller')
      federatedCredential(issuer, 'kyverno', 'kyverno-reports-controller')
    ]
  }
}

module registryPull '../../../lib/bicep/registry-pull.bicep' = {
  scope: resourceGroup(names.rgShared)
  params: {
    registryName: names.containerRegistry
    principalId: identity.outputs.principalId
    repositoryPermissions: profile.registry.repositoryPermissions
  }
}

output clientId string = identity.outputs.clientId
