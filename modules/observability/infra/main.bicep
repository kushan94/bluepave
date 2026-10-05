// Grafana queries managed Prometheus as this identity: Monitoring Data Reader on the environment's
// Azure Monitor workspace (read-only), federated to the observability:grafana service account.
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

resource monitorWorkspace 'Microsoft.Monitor/accounts@2025-10-03' existing = {
  name: names.monitorWorkspace
  scope: resourceGroup(names.rgManagement)
}

module identity 'br/public:avm/res/managed-identity/user-assigned-identity:0.6.0' = {
  scope: resourceGroup(names.rgAks)
  params: {
    name: workloadIdentityName(environmentName, 'grafana')
    location: location
    tags: tags
    federatedIdentityCredentials: [
      federatedCredential(cluster.properties.oidcIssuerProfile.issuerURL, 'observability', 'grafana')
    ]
  }
}

module metricsReader 'monitor-data-reader.bicep' = {
  scope: resourceGroup(names.rgManagement)
  params: {
    monitorWorkspaceName: names.monitorWorkspace
    principalId: identity.outputs.principalId
  }
}

output clientId string = identity.outputs.clientId
output monitorWorkspaceQueryEndpoint string = monitorWorkspace.properties.metrics.prometheusQueryEndpoint
