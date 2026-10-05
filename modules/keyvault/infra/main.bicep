// Per-environment Key Vault for platform secrets (deploy keys, the GitHub App key, the portal's
// Entra client secret). RBAC only, no access policies; External Secrets reads it with Workload
// Identity.
targetScope = 'subscription'

import {
  adminsGroupObjectId
  envName
  location
  privateDnsZoneNames
  privateEndpoint
  profile
  resourceNames
  subnetNames
  tagsFor
} from '../../../lib/bicep/platform.bicep'

@description('Environment to deploy.')
param environmentName envName

var names = resourceNames(environmentName, subscription().subscriptionId)
var tags = tagsFor(environmentName)
var publicAccess = profile.network.allowPublicNetworkAccess

resource rg 'Microsoft.Resources/resourceGroups@2025-04-01' = {
  name: names.rgShared
  location: location
  tags: tags
}

resource logAnalytics 'Microsoft.OperationalInsights/workspaces@2025-07-01' existing = {
  name: names.logAnalytics
  scope: resourceGroup(names.rgManagement)
}

resource spokeVnet 'Microsoft.Network/virtualNetworks@2025-05-01' existing = {
  name: names.spokeVnet
  scope: resourceGroup(names.rgNetwork)
  resource privateEndpointSubnet 'subnets' existing = {
    name: subnetNames.privateEndpoints
  }
}

resource keyVaultDnsZone 'Microsoft.Network/privateDnsZones@2024-06-01' existing = {
  name: privateDnsZoneNames.keyVault
  scope: resourceGroup(names.rgNetwork)
}

module keyVault 'br/public:avm/res/key-vault/vault:0.14.2' = {
  scope: rg
  params: {
    name: names.keyVault
    location: location
    tags: tags
    sku: 'standard'
    enableRbacAuthorization: true
    // Purge protection can't be turned off once on, and it blocks re-creating a vault with the
    // same name during the retention period; the trial profile leaves it off.
    enablePurgeProtection: profile.keyVault.purgeProtection
    softDeleteRetentionInDays: profile.keyVault.softDeleteDays
    publicNetworkAccess: publicAccess ? 'Enabled' : 'Disabled'
    networkAcls: {
      bypass: 'AzureServices'
      defaultAction: publicAccess ? 'Allow' : 'Deny'
    }
    privateEndpoints: profile.network.privateEndpoints
      ? [privateEndpoint(spokeVnet::privateEndpointSubnet.id, keyVaultDnsZone.id, tags)]
      : []
    roleAssignments: empty(adminsGroupObjectId)
      ? []
      : [{ principalId: adminsGroupObjectId, principalType: 'Group', roleDefinitionIdOrName: 'Key Vault Administrator' }]
    diagnosticSettings: [{ workspaceResourceId: logAnalytics.id }]
  }
}

output keyVaultName string = keyVault.outputs.name
output keyVaultUri string = keyVault.outputs.uri
