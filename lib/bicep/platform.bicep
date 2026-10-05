// The platform's configuration for Bicep: bluepave.yaml and the active profile, read at compile
// time, plus the naming and tagging conventions every module uses (ADR-0001). Modules import
// from here and never read bluepave.yaml themselves.

// any(): optional fields (network, budget) may be absent; the CLI validates the file's schema.
var platform = any(loadYamlContent('../../bluepave.yaml'))
// IDs written by `bluepave up` (empty in a fresh repository), under the key `discovered`.
var discoveredIds = any(loadYamlContent('../../.bluepave/discovered.yaml')).?discovered ?? {}
var profiles = {
  trial: loadYamlContent('../../profiles/trial.yaml').spec
  standard: loadYamlContent('../../profiles/standard.yaml').spec
  production: loadYamlContent('../../profiles/production.yaml').spec
}

@export()
@description('Environments a module can be deployed to.')
type envName = 'dev' | 'prod'

@export()
@description('bluepave.yaml spec.')
var spec = platform.spec

@export()
@description('The platform\'s name (bluepave.yaml metadata.name).')
var platformName = platform.metadata.name

@export()
@description('The active profile\'s settings (profiles/<spec.profile>.yaml spec).')
var profile = profiles[platform.spec.profile]

@export()
@description('Short prefix in every resource name.')
var prefix = platform.spec.prefix

@export()
@description('Azure region of every environment.')
var location = platform.spec.azure.region

// Short region codes, following the Cloud Adoption Framework's abbreviations where there is one.
var regionCodes = {
  australiaeast: 'ae'
  australiasoutheast: 'ase'
  brazilsouth: 'brs'
  canadacentral: 'cac'
  canadaeast: 'cae'
  centralindia: 'inc'
  centralus: 'cus'
  eastasia: 'ea'
  eastus: 'eus'
  eastus2: 'eus2'
  francecentral: 'frc'
  germanywestcentral: 'gwc'
  italynorth: 'itn'
  japaneast: 'jpe'
  japanwest: 'jpw'
  koreacentral: 'krc'
  northcentralus: 'ncus'
  northeurope: 'ne'
  norwayeast: 'nwe'
  polandcentral: 'plc'
  southafricanorth: 'san'
  southcentralus: 'scus'
  southeastasia: 'sea'
  southindia: 'ins'
  swedencentral: 'sdc'
  switzerlandnorth: 'szn'
  uaenorth: 'uan'
  uksouth: 'uks'
  ukwest: 'ukw'
  westeurope: 'we'
  westus: 'wus'
  westus2: 'wus2'
  westus3: 'wus3'
}

@export()
@description('Short code of the region in resource names; unknown regions use their first four letters.')
var regionCode = regionCodes[?location] ?? take(location, 4)

@export()
@description('Tags on every resource group and resource.')
func tagsFor(env string) object => {
  platform: platform.metadata.name
  env: env
  managedBy: 'bluepave'
}

@export()
@description('''Resource names: <type>-<prefix>-<env>-<region>[-<purpose>]. Globally unique names get a short
hash of the subscription, so two platforms with the same prefix don't collide.''')
func resourceNames(env string, subscriptionId string) object => {
  rgGlobal: 'rg-${prefix}-global-${regionCode}'
  rgManagement: 'rg-${prefix}-${env}-${regionCode}-mgmt'
  rgNetwork: 'rg-${prefix}-${env}-${regionCode}-network'
  rgShared: 'rg-${prefix}-${env}-${regionCode}-shared'
  rgAks: 'rg-${prefix}-${env}-${regionCode}-aks'
  rgAksNodes: 'rg-${prefix}-${env}-${regionCode}-aks-nodes'
  rgData: 'rg-${prefix}-${env}-${regionCode}-data'
  logAnalytics: 'log-${prefix}-${env}-${regionCode}'
  monitorWorkspace: 'amw-${prefix}-${env}-${regionCode}'
  hubVnet: 'vnet-${prefix}-${env}-${regionCode}-hub'
  spokeVnet: 'vnet-${prefix}-${env}-${regionCode}-spoke'
  nsgPrefix: 'nsg-${prefix}-${env}-${regionCode}'
  natGateway: 'ng-${prefix}-${env}-${regionCode}'
  natPublicIp: 'pip-${prefix}-${env}-${regionCode}-ng'
  firewall: 'afw-${prefix}-${env}-${regionCode}'
  firewallPolicy: 'afwp-${prefix}-${env}-${regionCode}'
  firewallPublicIp: 'pip-${prefix}-${env}-${regionCode}-afw'
  routeTable: 'rt-${prefix}-${env}-${regionCode}-spoke-egress'
  aksCluster: 'aks-${prefix}-${env}-${regionCode}'
  aksIdentity: 'id-${prefix}-${env}-${regionCode}-aks'
  // Globally unique: ACR allows only alphanumerics (5-50), Key Vault 3-24 characters.
  containerRegistry: 'cr${prefix}${env}${regionCode}${take(uniqueString(subscriptionId, prefix, env), 5)}'
  keyVault: 'kv-${prefix}-${env}-${regionCode}-${take(uniqueString(subscriptionId, prefix, env), 5)}'
  postgres: 'psql-${prefix}-${env}-${regionCode}-${take(uniqueString(subscriptionId, prefix, env), 5)}'
}

var defaultNetwork = {
  dev: { hubAddressPrefix: '10.10.0.0/22', spokeAddressPrefix: '10.11.0.0/16' }
  prod: { hubAddressPrefix: '10.20.0.0/22', spokeAddressPrefix: '10.21.0.0/16' }
}

@export()
@description('Address spaces of an environment: bluepave.yaml spec.network.<env>, else the defaults.')
func networkFor(env string) object => union(defaultNetwork[env], platform.spec.?network[?env] ?? {})

@export()
@description('Monthly budget of an environment: bluepave.yaml spec.budget.monthly.<env>, else the profile\'s.')
func budgetFor(env string) int => platform.spec.?budget.?monthly[?env] ?? profiles[platform.spec.profile].budget.monthly

@export()
@description('Extra e-mail addresses for budget alerts (bluepave.yaml spec.budget.contactEmails).')
var budgetContactEmails = platform.spec.?budget.?contactEmails ?? []

@export()
@description('The environments this platform has (bluepave.yaml spec.environments, default [dev]).')
var environmentNames = platform.spec.?environments ?? ['dev']

@export()
@description('The public DNS domain (bluepave.yaml spec.dns.domain).')
var dnsDomain = platform.spec.dns.domain

@export()
@description('Fixed subnet names, so modules can look subnets up with `existing`.')
var subnetNames = {
  firewall: 'AzureFirewallSubnet'
  aksNodes: 'snet-aks-nodes'
  aksApiServer: 'snet-aks-apiserver'
  agc: 'snet-agc'
  privateEndpoints: 'snet-private-endpoints'
}

@export()
@description('Private DNS zones for every Private Link service the platform uses.')
var privateDnsZoneNames = {
  acr: 'privatelink.azurecr.io'
  keyVault: 'privatelink.vaultcore.azure.net'
  blob: 'privatelink.blob.${environment().suffixes.storage}'
  postgres: 'privatelink.postgres.database.azure.com'
  redis: 'privatelink.redis.azure.net'
}

@export()
@description('Object ID of the platform admins group (discovered); empty before `bluepave up`.')
var adminsGroupObjectId = discoveredIds.?admins.?groupObjectId ?? ''

@export()
@description('Object ID of the CI identity that deploys and publishes (discovered); empty before `bluepave up`.')
var ciPrincipalId = discoveredIds.?ci.?principalId ?? ''

@export()
@description('One private endpoint in a subnet, registered in a private DNS zone (AVM privateEndpoints item).')
func privateEndpoint(subnetId string, dnsZoneId string, tags object) object => {
  subnetResourceId: subnetId
  privateDnsZoneGroup: {
    privateDnsZoneGroupConfigs: [{ privateDnsZoneResourceId: dnsZoneId }]
  }
  tags: tags
}

@export()
@description('A module\'s settings from bluepave.yaml (spec.modules.<name>.settings), or {}.')
func moduleSettings(name string) object => platform.spec.?modules[?name].?settings ?? {}
