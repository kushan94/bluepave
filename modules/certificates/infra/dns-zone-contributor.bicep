// Lets a workload identity manage records in one public DNS zone (and nothing else).

@description('DNS zone name, e.g. dev.example.com.')
param zoneName string

@description('Principal ID of the identity.')
param principalId string

var dnsZoneContributorRoleId = subscriptionResourceId(
  'Microsoft.Authorization/roleDefinitions',
  'befefa01-2a29-4197-83a8-272ff33ce314'
)

#disable-next-line use-recent-api-versions
resource zone 'Microsoft.Network/dnsZones@2018-05-01' existing = {
  name: zoneName
}

resource access 'Microsoft.Authorization/roleAssignments@2022-04-01' = {
  name: guid(zone.id, principalId, dnsZoneContributorRoleId)
  scope: zone
  properties: {
    principalId: principalId
    principalType: 'ServicePrincipal'
    roleDefinitionId: dnsZoneContributorRoleId
  }
}
