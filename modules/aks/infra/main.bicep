// Per-environment AKS cluster in the spoke, wired to what the modules it requires created:
//   - nodes in snet-aks-nodes, API server in snet-aks-apiserver (VNet integration) (network)
//   - egress through the profile's path: AKS load balancer, NAT gateway or firewall (network)
//   - images pulled from the environment's registry with the kubelet identity (registry)
//   - control-plane logs and Prometheus metrics to the environment's workspaces (monitoring)
// Identities for add-ons (External Secrets, Kargo, cert-manager, ...) belong to their own modules.
targetScope = 'subscription'

import {
  adminsGroupObjectId
  envName
  location
  moduleSettings
  profile
  resourceNames
  subnetNames
  tagsFor
} from '../../../lib/bicep/platform.bicep'

@description('Environment to deploy.')
param environmentName envName

var names = resourceNames(environmentName, subscription().subscriptionId)
var tags = tagsFor(environmentName)
var maintenanceWindow = union(
  { dayOfWeek: 'Sunday', startTime: '02:00', durationHours: 4, utcOffset: '+00:00' },
  moduleSettings('aks').?maintenanceWindow ?? {}
)

resource rg 'Microsoft.Resources/resourceGroups@2025-04-01' = {
  name: names.rgAks
  location: location
  tags: tags
}

resource spokeVnet 'Microsoft.Network/virtualNetworks@2025-05-01' existing = {
  name: names.spokeVnet
  scope: resourceGroup(names.rgNetwork)
  resource nodeSubnet 'subnets' existing = {
    name: subnetNames.aksNodes
  }
  resource apiServerSubnet 'subnets' existing = {
    name: subnetNames.aksApiServer
  }
}

resource logAnalytics 'Microsoft.OperationalInsights/workspaces@2025-07-01' existing = {
  name: names.logAnalytics
  scope: resourceGroup(names.rgManagement)
}

resource monitorWorkspace 'Microsoft.Monitor/accounts@2025-10-03' existing = {
  name: names.monitorWorkspace
  scope: resourceGroup(names.rgManagement)
}

// The control plane runs as this identity. It must exist (and have network access) before the
// cluster, which is why it is not a system-assigned identity.
module controlPlaneIdentity 'br/public:avm/res/managed-identity/user-assigned-identity:0.6.0' = {
  scope: rg
  params: {
    name: names.aksIdentity
    location: location
    tags: tags
  }
}

module networkAccess 'network-access.bicep' = {
  scope: resourceGroup(names.rgNetwork)
  params: {
    vnetName: names.spokeVnet
    routeTableName: profile.network.egress == 'firewall' ? names.routeTable : ''
    principalId: controlPlaneIdentity.outputs.principalId
  }
}

module cluster 'cluster.bicep' = {
  scope: rg
  dependsOn: [networkAccess]
  params: {
    name: names.aksCluster
    location: location
    tags: tags
    config: profile.cluster
    egress: profile.network.egress
    controlPlaneIdentityId: controlPlaneIdentity.outputs.resourceId
    nodeSubnetId: spokeVnet::nodeSubnet.id
    apiServerSubnetId: spokeVnet::apiServerSubnet.id
    nodeResourceGroup: names.rgAksNodes
    monitorWorkspaceId: monitorWorkspace.id
    logAnalyticsWorkspaceId: logAnalytics.id
    platformAdminsGroupId: adminsGroupObjectId
    maintenanceWindow: maintenanceWindow
  }
}

module registryPull 'registry-pull.bicep' = {
  scope: resourceGroup(names.rgShared)
  params: {
    registryName: names.containerRegistry
    principalId: cluster.outputs.kubeletIdentityObjectId
    repositoryPermissions: profile.registry.repositoryPermissions
  }
}

output clusterName string = cluster.outputs.name
output oidcIssuerUrl string = cluster.outputs.oidcIssuerUrl
output kubeletIdentityObjectId string = cluster.outputs.kubeletIdentityObjectId
