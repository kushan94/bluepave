// ASO's roles on the apps resource group (the resource group this module is deployed to).

@description('Object ID of ASO\'s identity.')
param principalId string

@description('Role definition GUIDs to grant without conditions.')
param roleIds string[]

@description('ABAC condition limiting which roles ASO may grant with Role Based Access Control Administrator.')
param rbacAdminCondition string

var rbacAdminRoleId = 'f58310d9-a9f6-439a-9e8d-f62e7b41a168'

resource assignments 'Microsoft.Authorization/roleAssignments@2022-04-01' = [
  for roleId in roleIds: {
    name: guid(resourceGroup().id, principalId, roleId)
    properties: {
      principalId: principalId
      principalType: 'ServicePrincipal'
      roleDefinitionId: subscriptionResourceId('Microsoft.Authorization/roleDefinitions', roleId)
    }
  }
]

resource rbacAdmin 'Microsoft.Authorization/roleAssignments@2022-04-01' = {
  name: guid(resourceGroup().id, principalId, rbacAdminRoleId)
  properties: {
    principalId: principalId
    principalType: 'ServicePrincipal'
    roleDefinitionId: subscriptionResourceId('Microsoft.Authorization/roleDefinitions', rbacAdminRoleId)
    condition: rbacAdminCondition
    conditionVersion: '2.0'
  }
}
