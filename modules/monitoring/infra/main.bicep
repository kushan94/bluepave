// Per-environment monitoring foundation: Log Analytics (logs, AKS control-plane logs) and an
// Azure Monitor workspace (managed Prometheus metrics).
targetScope = 'subscription'

import { envName, location, profile, resourceNames, tagsFor } from '../../../lib/bicep/platform.bicep'

@description('Environment to deploy.')
param environmentName envName

var names = resourceNames(environmentName, subscription().subscriptionId)
var tags = tagsFor(environmentName)

resource rg 'Microsoft.Resources/resourceGroups@2025-04-01' = {
  name: names.rgManagement
  location: location
  tags: tags
}

module logAnalytics 'br/public:avm/res/operational-insights/workspace:0.16.1' = {
  scope: rg
  params: {
    name: names.logAnalytics
    location: location
    tags: tags
    skuName: 'PerGB2018'
    dataRetention: profile.logs.retentionDays
    dailyQuotaGb: string(profile.logs.dailyQuotaGb)
  }
}

module monitorWorkspace 'monitor-workspace.bicep' = {
  scope: rg
  params: {
    name: names.monitorWorkspace
    location: location
    tags: tags
  }
}

output logAnalyticsWorkspaceId string = logAnalytics.outputs.resourceId
output monitorWorkspaceId string = monitorWorkspace.outputs.resourceId
