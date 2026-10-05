// Lets an identity query (only read) an Azure Monitor workspace's Prometheus metrics.

@description('Azure Monitor workspace name.')
param monitorWorkspaceName string

@description('Principal ID of the reader.')
param principalId string

var monitoringDataReaderRoleId = subscriptionResourceId(
  'Microsoft.Authorization/roleDefinitions',
  'b0d8363b-8ddd-447d-831f-62ca05bff136'
)

resource workspace 'Microsoft.Monitor/accounts@2025-10-03' existing = {
  name: monitorWorkspaceName
}

resource reader 'Microsoft.Authorization/roleAssignments@2022-04-01' = {
  name: guid(workspace.id, principalId, monitoringDataReaderRoleId)
  scope: workspace
  properties: {
    principalId: principalId
    principalType: 'ServicePrincipal'
    roleDefinitionId: monitoringDataReaderRoleId
  }
}
