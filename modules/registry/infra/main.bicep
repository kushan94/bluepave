// Per-environment Azure Container Registry. Entra ID is the only way in (no admin user, no anonymous
// pull). With the profile's registry.repositoryPermissions (ABAC), roles can be limited to
// repositories by name, so each app's CI pushes only its own <app>-* images.
targetScope = 'subscription'

import {
  adminsGroupObjectId
  ciPrincipalId
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

// In repository-permissions mode ACR ignores AcrPush; Container Registry Repository Writer (no
// condition) is its equivalent for all repositories.
var pushRole = profile.registry.repositoryPermissions ? '2a1e307c-b015-4ebd-883e-5b7698a07328' : 'AcrPush'
var pushers = concat(
  empty(adminsGroupObjectId) ? [] : [{ principalId: adminsGroupObjectId, principalType: 'Group' }],
  empty(ciPrincipalId) ? [] : [{ principalId: ciPrincipalId, principalType: 'ServicePrincipal' }]
)

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

resource acrDnsZone 'Microsoft.Network/privateDnsZones@2024-06-01' existing = {
  name: privateDnsZoneNames.acr
  scope: resourceGroup(names.rgNetwork)
}

// Premium is needed for private endpoints, zone redundancy and retention policies; with Standard
// the AVM module leaves those settings out.
module registry 'br/public:avm/res/container-registry/registry:0.13.1' = {
  scope: rg
  params: {
    name: names.containerRegistry
    location: location
    tags: tags
    acrSku: profile.registry.sku
    zoneRedundancy: 'Enabled'
    acrAdminUserEnabled: false
    anonymousPullEnabled: false
    roleAssignmentMode: profile.registry.repositoryPermissions ? 'AbacRepositoryPermissions' : 'LegacyRegistryPermissions'
    // Kyverno's Azure credential provider can only request ARM-audience tokens; the profile turns
    // them on only where image signatures are verified that way.
    azureADAuthenticationAsArmPolicyStatus: profile.registry.armTokenAuth ? 'enabled' : 'disabled'
    publicNetworkAccess: publicAccess ? 'Enabled' : 'Disabled'
    networkRuleSetDefaultAction: publicAccess ? 'Allow' : 'Deny'
    networkRuleBypassOptions: 'AzureServices'
    // Export can only be disabled when public access is.
    exportPolicyStatus: publicAccess ? 'enabled' : 'disabled'
    // Delete untagged manifests after 7 days.
    retentionPolicyStatus: 'enabled'
    retentionPolicyDays: 7
    privateEndpoints: profile.network.privateEndpoints
      ? [privateEndpoint(spokeVnet::privateEndpointSubnet.id, acrDnsZone.id, tags)]
      : []
    roleAssignments: map(pushers, p => union(p, { roleDefinitionIdOrName: pushRole }))
    diagnosticSettings: [{ workspaceResourceId: logAnalytics.id }]
  }
}

output containerRegistryName string = registry.outputs.name
output containerRegistryLoginServer string = registry.outputs.loginServer
