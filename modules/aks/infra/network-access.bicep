// Lets the AKS control-plane identity manage what it needs in the network resource group:
// joining nodes to the spoke subnets, and (with firewall egress) using the route table.

@description('Spoke VNet name.')
param vnetName string

@description('Route table name when egress goes through the firewall; empty otherwise.')
param routeTableName string = ''

@description('Principal ID of the AKS control-plane identity.')
param principalId string

var networkContributorRoleId = subscriptionResourceId(
  'Microsoft.Authorization/roleDefinitions',
  '4d97b98b-1d4f-4787-a291-c67834d212e7'
)

resource vnet 'Microsoft.Network/virtualNetworks@2025-05-01' existing = {
  name: vnetName
}

resource routeTable 'Microsoft.Network/routeTables@2025-05-01' existing = if (!empty(routeTableName)) {
  name: routeTableName
}

resource vnetAccess 'Microsoft.Authorization/roleAssignments@2022-04-01' = {
  name: guid(vnet.id, principalId, networkContributorRoleId)
  scope: vnet
  properties: {
    principalId: principalId
    principalType: 'ServicePrincipal'
    roleDefinitionId: networkContributorRoleId
  }
}

resource routeTableAccess 'Microsoft.Authorization/roleAssignments@2022-04-01' = if (!empty(routeTableName)) {
  name: guid(routeTable.id, principalId, networkContributorRoleId)
  scope: routeTable
  properties: {
    principalId: principalId
    principalType: 'ServicePrincipal'
    roleDefinitionId: networkContributorRoleId
  }
}
