// The identity External Secrets reads Key Vault as. Its federated credential trusts tokens the
// cluster's OIDC issuer signs for the external-secrets service account, so no secret is stored
// anywhere; it may only read secret values from this environment's Key Vault.
targetScope = 'subscription'

import {
  envName
  federatedCredential
  location
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
    name: workloadIdentityName(environmentName, 'external-secrets')
    location: location
    tags: tags
    federatedIdentityCredentials: [
      federatedCredential(cluster.properties.oidcIssuerProfile.issuerURL, 'external-secrets', 'external-secrets')
    ]
  }
}

module keyVaultAccess 'keyvault-secrets-user.bicep' = {
  scope: resourceGroup(names.rgShared)
  params: {
    keyVaultName: names.keyVault
    principalId: identity.outputs.principalId
  }
}

output clientId string = identity.outputs.clientId
