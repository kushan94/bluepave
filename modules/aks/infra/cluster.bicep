// AKS cluster with everything that lives in its resource group: extra node pools, maintenance
// windows, managed Prometheus wiring and cluster-scoped role assignments.
//
// Written against the raw resource (not the AVM module) so every setting of the cluster is visible
// here; this is the part of the platform worth reading line by line.

@description('Cluster name.')
param name string

@description('Azure region.')
param location string

@description('Resource tags.')
param tags object

@description('The profile\'s cluster settings (profiles/<name>.yaml spec.cluster).')
param config object

@description('Egress path, which decides the cluster outboundType.')
param egress 'loadBalancer' | 'natGateway' | 'firewall'

@description('Resource ID of the user-assigned identity the control plane runs as.')
param controlPlaneIdentityId string

@description('Subnet for the nodes (pods use the CNI Overlay range, not this subnet).')
param nodeSubnetId string

@description('Subnet delegated to AKS for API Server VNet Integration.')
param apiServerSubnetId string

@description('Name of the resource group AKS creates for nodes, load balancers and disks.')
param nodeResourceGroup string

@description('Azure Monitor workspace that receives Prometheus metrics.')
param monitorWorkspaceId string

@description('Log Analytics workspace for control-plane logs.')
param logAnalyticsWorkspaceId string

@description('Object ID of the Entra group that administers the platform. Empty skips the role assignments.')
param platformAdminsGroupId string = ''

@description('When automatic upgrades may run (the aks module\'s maintenanceWindow setting, already defaulted).')
param maintenanceWindow object

// CNI Overlay ranges. Pods and services never take VNet addresses, so they only need to avoid
// overlapping the VNets (bluepave.yaml spec.network).
var podCidr = '10.244.0.0/16'
var serviceCidr = '10.0.0.0/16'
var dnsServiceIp = '10.0.0.10'

var outboundType = {
  loadBalancer: 'loadBalancer'
  natGateway: 'userAssignedNATGateway'
  firewall: 'userDefinedRouting'
}[egress]

var zones = ['1', '2', '3']

// Ephemeral OS disks live on the VM's local temp disk: faster, free, and re-imaged on every
// upgrade. D*ds_v5 sizes have >= 75 GB of temp disk, so a 64 GB OS disk fits.
var osDisk = {
  osDiskType: 'Ephemeral'
  osDiskSizeGB: 64
  osSKU: 'AzureLinux'
}

resource cluster 'Microsoft.ContainerService/managedClusters@2026-05-01' = {
  name: name
  location: location
  tags: tags
  sku: {
    name: 'Base'
    tier: config.tier
  }
  identity: {
    type: 'UserAssigned'
    userAssignedIdentities: {
      '${controlPlaneIdentityId}': {}
    }
  }
  properties: {
    kubernetesVersion: config.kubernetesVersion
    dnsPrefix: name
    nodeResourceGroup: nodeResourceGroup
    // Nobody can change the node resource group directly; changes go through the AKS API.
    nodeResourceGroupProfile: {
      restrictionLevel: 'ReadOnly'
    }
    supportPlan: 'KubernetesOfficial'

    // --- Identity and access ------------------------------------------------------------------
    // Entra ID is the only way in: no static admin kubeconfig, and Kubernetes RBAC decisions are
    // made by Azure RBAC role assignments.
    enableRBAC: true
    disableLocalAccounts: true
    aadProfile: {
      managed: true
      enableAzureRBAC: true
      tenantID: tenant().tenantId
    }
    // OIDC issuer + Workload Identity: pods get Entra tokens through federated credentials, so
    // apps and add-ons never hold secrets for Azure.
    oidcIssuerProfile: {
      enabled: true
    }
    securityProfile: {
      workloadIdentity: {
        enabled: true
      }
      imageCleaner: {
        enabled: config.addons.imageCleaner
        intervalHours: 168
      }
    }

    // --- API server ---------------------------------------------------------------------------
    // API Server VNet Integration puts the API server's endpoint in our delegated subnet, so nodes
    // reach it privately. Unless the profile makes the cluster private, it is also public (Entra
    // auth only).
    apiServerAccessProfile: {
      enableVnetIntegration: true
      subnetId: apiServerSubnetId
      enablePrivateCluster: config.privateCluster
    }

    // --- Networking ---------------------------------------------------------------------------
    networkProfile: {
      networkPlugin: 'azure'
      networkPluginMode: 'overlay'
      networkDataplane: 'cilium'
      networkPolicy: 'cilium'
      podCidr: podCidr
      serviceCidr: serviceCidr
      dnsServiceIP: dnsServiceIp
      loadBalancerSku: 'standard'
      outboundType: outboundType
      advancedNetworking: config.advancedNetworking
        ? {
            enabled: true
            observability: { enabled: true }
            security: { enabled: true }
          }
        : null
    }

    // --- Node pools ---------------------------------------------------------------------------
    // Only the system pool is declared inline; user pools are child resources below, so they can
    // be added and removed without touching the cluster resource.
    agentPoolProfiles: [
      union(osDisk, {
        name: 'system'
        mode: 'System'
        type: 'VirtualMachineScaleSets'
        orchestratorVersion: config.kubernetesVersion
        vmSize: config.systemPool.vmSize
        count: config.systemPool.count
        enableAutoScaling: true
        minCount: config.systemPool.count
        maxCount: config.systemPool.maxCount
        availabilityZones: zones
        vnetSubnetID: nodeSubnetId
        maxPods: 110
        nodeTaints: config.systemPool.criticalAddonsOnly ? ['CriticalAddonsOnly=true:NoSchedule'] : []
        // Add one node, move pods, then remove the old node. System pools must surge (AKS rejects
        // maxUnavailable > 0 for them). trial: 1 node + 1 surge = 4 vCPUs, the Free Trial limit.
        upgradeSettings: {
          maxSurge: '1'
        }
      })
    ]

    // --- Upgrades -----------------------------------------------------------------------------
    // Patch versions and node images update automatically, inside the maintenance windows below,
    // unless the profile upgrades by hand (cluster.autoUpgrade).
    autoUpgradeProfile: {
      upgradeChannel: config.autoUpgrade ? 'patch' : 'none'
      nodeOSUpgradeChannel: config.autoUpgrade ? 'NodeImage' : 'None'
    }

    // --- Add-ons ------------------------------------------------------------------------------
    addonProfiles: {
      // Mount Key Vault secrets as files (an alternative to External Secrets).
      azureKeyvaultSecretsProvider: {
        enabled: true
        config: {
          enableSecretRotation: 'true'
        }
      }
      // Azure Policy for Kubernetes (Gatekeeper), also needed by Deployment Safeguards.
      azurepolicy: {
        enabled: true
      }
    }
    workloadAutoScalerProfile: {
      keda: {
        enabled: config.addons.keda
      }
      verticalPodAutoscaler: {
        enabled: config.addons.vpa
      }
    }
    storageProfile: {
      diskCSIDriver: { enabled: true }
      fileCSIDriver: { enabled: false }
      blobCSIDriver: { enabled: false }
      snapshotController: { enabled: true }
    }

    // Managed Prometheus: the AKS metrics agent scrapes the cluster and sends to the Azure
    // Monitor workspace through the data collection rule below.
    azureMonitorProfile: {
      metrics: {
        enabled: true
        kubeStateMetrics: {
          metricLabelsAllowlist: ''
          metricAnnotationsAllowList: ''
        }
      }
    }
  }
}

resource userPools 'Microsoft.ContainerService/managedClusters/agentPools@2026-05-01' = [
  for pool in config.userPools: {
    parent: cluster
    name: pool.name
    properties: union(osDisk, {
      tags: tags
      mode: 'User'
      type: 'VirtualMachineScaleSets'
      orchestratorVersion: config.kubernetesVersion
      vmSize: pool.vmSize
      availabilityZones: zones
      vnetSubnetID: nodeSubnetId
      maxPods: 110
      enableAutoScaling: true
      minCount: pool.minCount
      maxCount: pool.maxCount
      count: pool.minCount
    }, pool.spot
      ? {
          // AKS adds the taint kubernetes.azure.com/scalesetpriority=spot:NoSchedule itself.
          scaleSetPriority: 'Spot'
          scaleSetEvictionPolicy: 'Delete'
          spotMaxPrice: -1 // pay up to the on-demand price, never evicted for price
          // No upgradeSettings: AKS rejects maxUnavailable on Spot pools.
        }
      : {
          // Surge: add a node, move pods, remove the old one. Without spare quota: replace nodes
          // in place, one at a time (the pool's pods are briefly unavailable).
          upgradeSettings: pool.surge ? { maxSurge: '33%' } : { maxSurge: '0', maxUnavailable: '1' }
        })
  }
]

// Upgrades only happen in this window.
var maintenance = {
  maintenanceWindow: {
    schedule: {
      weekly: {
        dayOfWeek: maintenanceWindow.dayOfWeek
        intervalWeeks: 1
      }
    }
    durationHours: maintenanceWindow.durationHours
    startTime: maintenanceWindow.startTime
    utcOffset: maintenanceWindow.utcOffset
  }
}

resource clusterUpgradeWindow 'Microsoft.ContainerService/managedClusters/maintenanceConfigurations@2026-05-01' = {
  parent: cluster
  name: 'aksManagedAutoUpgradeSchedule'
  properties: maintenance
}

resource nodeOsUpgradeWindow 'Microsoft.ContainerService/managedClusters/maintenanceConfigurations@2026-05-01' = {
  parent: cluster
  name: 'aksManagedNodeOSUpgradeSchedule'
  properties: maintenance
}

// --- Control-plane logs -------------------------------------------------------------------------
// Audit log of every change made through the API server (reads are left out by kube-audit-admin),
// Entra authentication (guard), and the scheduler, controller manager and cluster autoscaler.
// kube-apiserver is left out: it is very chatty and would use up the trial profile's 1 GB/day cap.
// The only newer non-preview version (2016-09-01) is older and lacks logAnalyticsDestinationType.
#disable-next-line use-recent-api-versions
resource controlPlaneLogs 'Microsoft.Insights/diagnosticSettings@2021-05-01-preview' = {
  name: 'control-plane-logs'
  scope: cluster
  properties: {
    workspaceId: logAnalyticsWorkspaceId
    // Resource-specific tables (AKSAudit, AKSControlPlane) instead of the shared AzureDiagnostics.
    logAnalyticsDestinationType: 'Dedicated'
    logs: [
      for category in [
        'kube-audit-admin'
        'guard'
        'kube-scheduler'
        'kube-controller-manager'
        'cluster-autoscaler'
      ]: {
        category: category
        enabled: true
      }
    ]
  }
}

// --- Managed Prometheus wiring ------------------------------------------------------------------
// The portal creates these for you; in code we wire them ourselves:
//   data collection endpoint -> data collection rule (where to send) -> associations to the cluster
resource prometheusEndpoint 'Microsoft.Insights/dataCollectionEndpoints@2024-03-11' = {
  name: 'MSProm-${location}-${name}'
  location: location
  tags: tags
  kind: 'Linux'
  properties: {}
}

resource prometheusRule 'Microsoft.Insights/dataCollectionRules@2024-03-11' = {
  name: 'MSProm-${location}-${name}'
  location: location
  tags: tags
  kind: 'Linux'
  properties: {
    dataCollectionEndpointId: prometheusEndpoint.id
    dataSources: {
      prometheusForwarder: [
        {
          name: 'PrometheusDataSource'
          streams: ['Microsoft-PrometheusMetrics']
          labelIncludeFilter: {}
        }
      ]
    }
    destinations: {
      monitoringAccounts: [
        {
          name: 'MonitoringAccount1'
          accountResourceId: monitorWorkspaceId
        }
      ]
    }
    dataFlows: [
      {
        streams: ['Microsoft-PrometheusMetrics']
        destinations: ['MonitoringAccount1']
      }
    ]
  }
}

resource prometheusRuleAssociation 'Microsoft.Insights/dataCollectionRuleAssociations@2024-03-11' = {
  name: 'MSProm-${location}-${name}'
  scope: cluster
  properties: {
    dataCollectionRuleId: prometheusRule.id
  }
}

resource prometheusEndpointAssociation 'Microsoft.Insights/dataCollectionRuleAssociations@2024-03-11' = {
  // This exact name tells the metrics agent where to fetch its configuration.
  name: 'configurationAccessEndpoint'
  scope: cluster
  properties: {
    dataCollectionEndpointId: prometheusEndpoint.id
  }
}

// --- Access for platform admins -----------------------------------------------------------------
var adminRoles = {
  // Full access to every Kubernetes resource, through Azure RBAC.
  clusterAdmin: 'b1ff04bb-8a4e-4dc4-8eb5-8693973ce19b' // Azure Kubernetes Service RBAC Cluster Admin
  // Lets `az aks get-credentials` download a kubeconfig (which still authenticates with Entra).
  clusterUser: '4abbcc35-e782-43d8-92c5-2d3f1bd2253f' // Azure Kubernetes Service Cluster User Role
}

resource adminRoleAssignments 'Microsoft.Authorization/roleAssignments@2022-04-01' = [
  for role in items(adminRoles): if (!empty(platformAdminsGroupId)) {
    name: guid(cluster.id, platformAdminsGroupId, role.value)
    scope: cluster
    properties: {
      principalId: platformAdminsGroupId
      principalType: 'Group'
      roleDefinitionId: subscriptionResourceId('Microsoft.Authorization/roleDefinitions', role.value)
    }
  }
]

output name string = cluster.name
output resourceId string = cluster.id
output oidcIssuerUrl string = cluster.properties.oidcIssuerProfile.issuerURL
output kubeletIdentityObjectId string = cluster.properties.identityProfile.kubeletidentity.objectId
