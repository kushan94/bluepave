// The identity Kargo's controller watches the registry as: it reads image metadata (tags, digests,
// signatures) to discover new Freight, with no registry credentials anywhere. Its federated
// credential trusts the kargo-controller service account.
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

resource cluster 'Microsoft.ContainerService/managedClusters@2026-05-01' existing = {
  name: names.aksCluster
  scope: resourceGroup(names.rgAks)
}

module identity 'br/public:avm/res/managed-identity/user-assigned-identity:0.6.0' = {
  scope: resourceGroup(names.rgAks)
  params: {
    name: workloadIdentityName(environmentName, 'kargo')
    location: location
    tags: tags
    federatedIdentityCredentials: [
      federatedCredential(cluster.properties.oidcIssuerProfile.issuerURL, 'kargo', 'kargo-controller')
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
