// Per-environment hub-and-spoke network (ported from the reference implementation).
//
//   Hub VNet   : AzureFirewallSubnet (the firewall is deployed only when the profile's egress is
//                'firewall')
//   Spoke VNet : AKS nodes, AKS API server (VNet integration), Application Gateway for Containers,
//                private endpoints
//   Egress     : AKS load balancer (trial), NAT Gateway (standard), or Azure Firewall + route table
//                (production)
//   DNS        : private DNS zones for every Private Link service, linked to both VNets
targetScope = 'subscription'

import {
  envName
  location
  moduleSettings
  networkFor
  privateDnsZoneNames
  profile
  resourceNames
  subnetNames
  tagsFor
} from '../../../lib/bicep/platform.bicep'

@description('Environment to deploy.')
param environmentName envName

// FQDNs the cluster may reach through Azure Firewall on HTTPS, for public container registries and
// Git hosts used by platform add-ons. bluepave.yaml spec.modules.network.settings.allowedEgressFqdns
// replaces the list.
var defaultEgressFqdns = [
  'ghcr.io'
  'pkg-containers.githubusercontent.com'
  'quay.io'
  '*.quay.io'
  'registry.k8s.io'
  '*.pkg.dev'
  'docker.io'
  'registry-1.docker.io'
  'auth.docker.io'
  'production.cloudflare.docker.com'
  'github.com'
  'objects.githubusercontent.com'
  'raw.githubusercontent.com'
]

var allowedEgressFqdns = moduleSettings('network').?allowedEgressFqdns ?? defaultEgressFqdns
var addressSpace = networkFor(environmentName)
var names = resourceNames(environmentName, subscription().subscriptionId)
var tags = tagsFor(environmentName)
var useFirewall = profile.network.egress == 'firewall'
var useNatGateway = profile.network.egress == 'natGateway'

// Address plan, derived from the two VNet prefixes. With the dev defaults (10.10.0.0/22 hub, 10.11.0.0/16 spoke):
//   AzureFirewallSubnet     10.10.0.0/26
//   snet-aks-nodes          10.11.0.0/22   node IPs only: pods get IPs from the CNI Overlay range
//   snet-aks-apiserver      10.11.4.0/28   delegated to AKS for API Server VNet Integration
//   snet-agc                10.11.8.0/24   delegated to AGC, which needs at least a /24
//   snet-private-endpoints  10.11.9.0/24
var hubSubnetPrefixes = {
  firewall: cidrSubnet(addressSpace.hubAddressPrefix, 26, 0)
}
var spokeSubnetPrefixes = {
  aksNodes: cidrSubnet(addressSpace.spokeAddressPrefix, 22, 0)
  aksApiServer: cidrSubnet(addressSpace.spokeAddressPrefix, 28, 64)
  agc: cidrSubnet(addressSpace.spokeAddressPrefix, 24, 8)
  privateEndpoints: cidrSubnet(addressSpace.spokeAddressPrefix, 24, 9)
}

// AKS needs these Azure endpoints on top of the AzureKubernetesService FQDN tag once its add-ons are on.
var azureServiceFqdns = [
  // Azure Monitor: Container Insights and managed Prometheus
  '*.ods.opinsights.azure.com'
  '*.oms.opinsights.azure.com'
  'dc.services.visualstudio.com'
  '*.monitoring.azure.com'
  '*.ingest.monitor.azure.com'
  'global.handler.control.monitor.azure.com'
  '*.handler.control.monitor.azure.com'
  // Azure Policy add-on
  'data.policy.${environment().suffixes.storage}'
  'store.policy.${environment().suffixes.storage}'
  // Microsoft Defender for Containers
  '*.cloud.defender.microsoft.com'
]

var diagnostics = [
  { workspaceResourceId: logAnalytics.id }
]

resource rg 'Microsoft.Resources/resourceGroups@2025-04-01' = {
  name: names.rgNetwork
  location: location
  tags: tags
}

resource logAnalytics 'Microsoft.OperationalInsights/workspaces@2025-07-01' existing = {
  name: names.logAnalytics
  scope: resourceGroup(names.rgManagement)
}

// ---------------------------------------------------------------------------------------------
// Network security groups, one per spoke subnet (AzureFirewallSubnet cannot have one)
// ---------------------------------------------------------------------------------------------
var nsgSubnets = [
  subnetNames.aksNodes
  subnetNames.aksApiServer
  subnetNames.agc
  subnetNames.privateEndpoints
]

// Nothing in the spoke needs SSH or RDP to its neighbours, so block it to limit an attacker's
// lateral movement after compromising one workload.
var denyLateralMovementRule = {
  name: 'deny-ssh-rdp-outbound'
  properties: {
    access: 'Deny'
    direction: 'Outbound'
    priority: 4000
    protocol: 'Tcp'
    sourceAddressPrefix: '*'
    sourcePortRange: '*'
    destinationAddressPrefix: 'VirtualNetwork'
    destinationPortRanges: ['22', '3389']
  }
}

var allowWebFromInternet = {
  name: 'allow-http-https-inbound'
  properties: {
    access: 'Allow'
    direction: 'Inbound'
    priority: 100
    protocol: 'Tcp'
    sourceAddressPrefix: 'Internet'
    sourcePortRange: '*'
    destinationAddressPrefix: '*'
    destinationPortRanges: ['80', '443']
  }
}

// Web traffic enters where the gateway runs:
//   agc       -> AGC frontends on the delegated subnet (NSGs enforced there since April 2026)
//   inCluster -> Envoy Gateway pods on the nodes, behind AKS's public load balancer
var subnetRules = profile.ingress == 'agc'
  ? { '${subnetNames.agc}': [allowWebFromInternet] }
  : { '${subnetNames.aksNodes}': [allowWebFromInternet] }


module nsgs 'br/public:avm/res/network/network-security-group:0.5.3' = [
  for subnet in nsgSubnets: {
    scope: rg
    name: 'nsg-${subnet}'
    params: {
      name: '${names.nsgPrefix}-${replace(subnet, 'snet-', '')}'
      location: location
      tags: tags
      securityRules: concat(subnetRules[?subnet] ?? [], [denyLateralMovementRule])
      diagnosticSettings: diagnostics
    }
  }
]

// ---------------------------------------------------------------------------------------------
// Hub
// ---------------------------------------------------------------------------------------------
module hubVnet 'br/public:avm/res/network/virtual-network:0.10.2' = {
  scope: rg
  params: {
    name: names.hubVnet
    location: location
    tags: tags
    addressPrefixes: [addressSpace.hubAddressPrefix]
    subnets: [
      { name: subnetNames.firewall, addressPrefix: hubSubnetPrefixes.firewall }
    ]
    diagnosticSettings: diagnostics
  }
}

// ---------------------------------------------------------------------------------------------
// Egress 'loadBalancer' (trial) deploys nothing here: AKS creates outbound rules on its own
// Standard Load Balancer (outboundType: loadBalancer).
//
// Egress 'natGateway' (standard): NAT Gateway on the AKS node subnet. Not filtered or logged.
// A non-zonal NAT Gateway is a single point of failure.
// ---------------------------------------------------------------------------------------------
module natGateway 'br/public:avm/res/network/nat-gateway:2.1.1' = if (useNatGateway) {
  scope: rg
  params: {
    name: names.natGateway
    location: location
    tags: tags
    availabilityZone: -1
    publicIPAddresses: [
      { name: names.natPublicIp }
    ]
  }
}

// ---------------------------------------------------------------------------------------------
// Egress 'firewall' (production): zone-redundant Azure Firewall in the hub. Everything leaving the spoke
// is forced through it by a route table, and only the FQDNs below are allowed.
// ---------------------------------------------------------------------------------------------
module firewallPolicy 'br/public:avm/res/network/firewall-policy:0.3.6' = if (useFirewall) {
  scope: rg
  params: {
    name: names.firewallPolicy
    location: location
    tags: tags
    tier: 'Standard'
    threatIntelMode: 'Deny'
    ruleCollectionGroups: [
      {
        name: 'aks-egress'
        priority: 200
        ruleCollections: [
          {
            name: 'aks-network'
            priority: 100
            ruleCollectionType: 'FirewallPolicyFilterRuleCollection'
            action: { type: 'Allow' }
            rules: [
              {
                ruleType: 'NetworkRule'
                name: 'aks-tunnel-udp'
                ipProtocols: ['UDP']
                sourceAddresses: [spokeSubnetPrefixes.aksNodes]
                destinationAddresses: ['AzureCloud.${location}']
                destinationPorts: ['1194']
              }
              {
                ruleType: 'NetworkRule'
                name: 'aks-tunnel-tcp'
                ipProtocols: ['TCP']
                sourceAddresses: [spokeSubnetPrefixes.aksNodes]
                destinationAddresses: ['AzureCloud.${location}']
                destinationPorts: ['9000']
              }
            ]
          }
          {
            name: 'aks-application'
            priority: 200
            ruleCollectionType: 'FirewallPolicyFilterRuleCollection'
            action: { type: 'Allow' }
            rules: [
              {
                ruleType: 'ApplicationRule'
                name: 'aks-required'
                sourceAddresses: [spokeSubnetPrefixes.aksNodes]
                fqdnTags: ['AzureKubernetesService']
                protocols: [
                  { protocolType: 'Https', port: 443 }
                  { protocolType: 'Http', port: 80 }
                ]
              }
              {
                ruleType: 'ApplicationRule'
                name: 'azure-services'
                sourceAddresses: [spokeSubnetPrefixes.aksNodes]
                targetFqdns: azureServiceFqdns
                protocols: [{ protocolType: 'Https', port: 443 }]
              }
              {
                ruleType: 'ApplicationRule'
                name: 'platform-dependencies'
                sourceAddresses: [spokeSubnetPrefixes.aksNodes]
                targetFqdns: allowedEgressFqdns
                protocols: [{ protocolType: 'Https', port: 443 }]
              }
            ]
          }
        ]
      }
    ]
  }
}

module firewall 'br/public:avm/res/network/azure-firewall:0.11.1' = if (useFirewall) {
  scope: rg
  params: {
    name: names.firewall
    location: location
    tags: tags
    azureSkuTier: 'Standard'
    availabilityZones: [1, 2, 3]
    virtualNetworkResourceId: hubVnet.outputs.resourceId
    firewallPolicyId: firewallPolicy!.outputs.resourceId
    publicIPAddressObject: {
      name: names.firewallPublicIp
    }
    diagnosticSettings: diagnostics
  }
}

module routeTable 'br/public:avm/res/network/route-table:0.5.0' = if (useFirewall) {
  scope: rg
  params: {
    name: names.routeTable
    location: location
    tags: tags
    // Stop routes learned from gateways overriding the default route through the firewall.
    disableBgpRoutePropagation: true
    routes: [
      {
        name: 'default-via-firewall'
        properties: {
          addressPrefix: '0.0.0.0/0'
          nextHopType: 'VirtualAppliance'
          nextHopIpAddress: firewall!.outputs.privateIp
        }
      }
    ]
  }
}

// ---------------------------------------------------------------------------------------------
// Spoke
// ---------------------------------------------------------------------------------------------
module spokeVnet 'br/public:avm/res/network/virtual-network:0.10.2' = {
  scope: rg
  params: {
    name: names.spokeVnet
    location: location
    tags: tags
    addressPrefixes: [addressSpace.spokeAddressPrefix]
    subnets: [
      {
        name: subnetNames.aksNodes
        addressPrefix: spokeSubnetPrefixes.aksNodes
        networkSecurityGroupResourceId: nsgs[indexOf(nsgSubnets, subnetNames.aksNodes)].outputs.resourceId
        natGatewayResourceId: useNatGateway ? natGateway!.outputs.resourceId : null
        routeTableResourceId: useFirewall ? routeTable!.outputs.resourceId : null
        // Private subnet: no implicit internet access. Egress only through an explicit path
        // (AKS load balancer outbound rules, NAT Gateway or firewall).
        defaultOutboundAccess: false
      }
      {
        name: subnetNames.aksApiServer
        addressPrefix: spokeSubnetPrefixes.aksApiServer
        delegation: 'Microsoft.ContainerService/managedClusters'
        networkSecurityGroupResourceId: nsgs[indexOf(nsgSubnets, subnetNames.aksApiServer)].outputs.resourceId
        defaultOutboundAccess: false
      }
      {
        name: subnetNames.agc
        addressPrefix: spokeSubnetPrefixes.agc
        delegation: 'Microsoft.ServiceNetworking/trafficControllers'
        networkSecurityGroupResourceId: nsgs[indexOf(nsgSubnets, subnetNames.agc)].outputs.resourceId
        defaultOutboundAccess: false
      }
      {
        name: subnetNames.privateEndpoints
        addressPrefix: spokeSubnetPrefixes.privateEndpoints
        networkSecurityGroupResourceId: nsgs[indexOf(nsgSubnets, subnetNames.privateEndpoints)].outputs.resourceId
        // Apply NSG rules and route tables to private endpoint traffic too.
        privateEndpointNetworkPolicies: 'Enabled'
        defaultOutboundAccess: false
      }
    ]
    peerings: [
      {
        name: 'spoke-to-hub'
        remoteVirtualNetworkResourceId: hubVnet.outputs.resourceId
        allowVirtualNetworkAccess: true
        allowForwardedTraffic: true
        remotePeeringEnabled: true
        remotePeeringName: 'hub-to-spoke'
        remotePeeringAllowVirtualNetworkAccess: true
        remotePeeringAllowForwardedTraffic: true
      }
    ]
    diagnosticSettings: diagnostics
  }
}

// ---------------------------------------------------------------------------------------------
// Private DNS zones, so private endpoints resolve to private IPs from both VNets
// ---------------------------------------------------------------------------------------------
module privateDnsZones 'br/public:avm/res/network/private-dns-zone:0.8.1' = [
  for zone in items(privateDnsZoneNames): {
    scope: rg
    name: 'dns-${zone.key}'
    params: {
      name: zone.value
      tags: tags
      virtualNetworkLinks: [
        { name: 'link-${names.hubVnet}', virtualNetworkResourceId: hubVnet.outputs.resourceId }
        { name: 'link-${names.spokeVnet}', virtualNetworkResourceId: spokeVnet.outputs.resourceId }
      ]
    }
  }
]

output spokeVnetId string = spokeVnet.outputs.resourceId
output hubVnetId string = hubVnet.outputs.resourceId
output egress string = profile.network.egress
output firewallPrivateIp string = useFirewall ? firewall!.outputs.privateIp : ''
