// What Azure Service Operator (ASO) needs to create app resources that app teams request through
// the platform APIs (ADR-0003):
//   - the apps resource group, the only place ASO may create anything
//   - ASO's identity, federated to its service account (no client secret)
//   - on that resource group only: Managed Identity Contributor, Storage Account Contributor, and
//     Role Based Access Control Administrator limited by a condition to granting or removing the
//     data roles the APIs use, to service principals only (it can't make anyone Owner or grant
//     itself more)
// Granting RBAC Administrator needs Owner or User Access Administrator on the subscription.
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

// Data roles the platform APIs may grant to app identities.
var grantableRoles = [
  'ba92f5b4-2d11-453d-a403-e96b0029c9fe' // Storage Blob Data Contributor (AppStorage, readWrite)
  '2a2b9908-6ea1-4ae2-8e65-a410df84e7d1' // Storage Blob Data Reader (AppStorage, readOnly)
]
var roleList = join(grantableRoles, ',')
var rbacAdminCondition = '((!(ActionMatches{\'Microsoft.Authorization/roleAssignments/write\'})) OR (@Request[Microsoft.Authorization/roleAssignments:RoleDefinitionId] ForAnyOfAnyValues:GuidEquals {${roleList}} AND @Request[Microsoft.Authorization/roleAssignments:PrincipalType] ForAnyOfAnyValues:StringEqualsIgnoreCase {\'ServicePrincipal\'})) AND ((!(ActionMatches{\'Microsoft.Authorization/roleAssignments/delete\'})) OR (@Resource[Microsoft.Authorization/roleAssignments:RoleDefinitionId] ForAnyOfAnyValues:GuidEquals {${roleList}}))'

resource cluster 'Microsoft.ContainerService/managedClusters@2026-05-01' existing = {
  name: names.aksCluster
  scope: resourceGroup(names.rgAks)
}

resource appsRg 'Microsoft.Resources/resourceGroups@2025-04-01' = {
  name: names.rgApps
  location: location
  tags: tags
}

module identity 'br/public:avm/res/managed-identity/user-assigned-identity:0.6.0' = {
  scope: resourceGroup(names.rgAks)
  params: {
    name: workloadIdentityName(environmentName, 'aso')
    location: location
    tags: tags
    federatedIdentityCredentials: [
      federatedCredential(
        cluster.properties.oidcIssuerProfile.issuerURL,
        'azureserviceoperator-system',
        'azureserviceoperator-default'
      )
    ]
  }
}

module appsRoles 'apps-roles.bicep' = {
  scope: appsRg
  params: {
    principalId: identity.outputs.principalId
    roleIds: [
      'e40ec5ca-96e0-45a2-b4ff-59039f2c2b59' // Managed Identity Contributor
      '17d1049b-9a84-46fb-8f53-869881c3d3ab' // Storage Account Contributor
    ]
    rbacAdminCondition: rbacAdminCondition
  }
}

output clientId string = identity.outputs.clientId
output appsResourceGroupId string = appsRg.id
output oidcIssuerUrl string = cluster.properties.oidcIssuerProfile.issuerURL
