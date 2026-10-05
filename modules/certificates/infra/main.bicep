// The identity cert-manager solves Let's Encrypt DNS-01 challenges as: it may change records in
// this environment's public subzone (<env>.<domain>, created by the dns module) and nothing else.
// Its federated credential trusts the cert-manager service account, so no secret is stored.
targetScope = 'subscription'

import {
  dnsDomain
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
var globalNames = resourceNames('global', subscription().subscriptionId)
var tags = tagsFor(environmentName)
var zoneName = '${environmentName}.${dnsDomain}'

resource cluster 'Microsoft.ContainerService/managedClusters@2026-05-01' existing = {
  name: names.aksCluster
  scope: resourceGroup(names.rgAks)
}

module identity 'br/public:avm/res/managed-identity/user-assigned-identity:0.6.0' = {
  scope: resourceGroup(names.rgAks)
  params: {
    name: workloadIdentityName(environmentName, 'cert-manager')
    location: location
    tags: tags
    federatedIdentityCredentials: [
      federatedCredential(cluster.properties.oidcIssuerProfile.issuerURL, 'cert-manager', 'cert-manager')
    ]
  }
}

module zoneAccess 'dns-zone-contributor.bicep' = {
  scope: resourceGroup(globalNames.rgGlobal)
  params: {
    zoneName: zoneName
    principalId: identity.outputs.principalId
  }
}

output clientId string = identity.outputs.clientId
output dnsZoneName string = zoneName
output dnsZoneResourceGroup string = globalNames.rgGlobal
