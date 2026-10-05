// Per-environment PostgreSQL Flexible Server:
//   - Entra ID authentication only: no passwords exist for this server. The platform admins group
//     is the Entra admin; apps sign in with their workload identities.
//   - reachable only through a private endpoint in the spoke, in every profile (a public database
//     behind an IP allow-list is fragile and less safe; the endpoint costs ~US$7/month)
//   - sizing, high availability and backups from the profile's data.postgres
targetScope = 'subscription'

import {
  adminsGroupObjectId
  envName
  location
  moduleSettings
  privateDnsZoneNames
  privateEndpoint
  profile
  resourceNames
  spec
  subnetNames
  tagsFor
} from '../../../lib/bicep/platform.bicep'

@description('Environment to deploy.')
param environmentName envName

var names = resourceNames(environmentName, subscription().subscriptionId)
var tags = tagsFor(environmentName)
var settings = moduleSettings('data-postgres')
var config = profile.data.postgres
var zoneRedundant = config.highAvailability == 'ZoneRedundant'
// Microsoft Defender for open-source databases is billed per server, so trial leaves it off.
var threatProtection = settings.?threatProtection ?? spec.profile != 'trial'
var maintenanceWindow = union({ dayOfWeek: 0, startHour: 2, startMinute: 0 }, settings.?maintenanceWindow ?? {})

resource rg 'Microsoft.Resources/resourceGroups@2025-04-01' = {
  name: names.rgData
  location: location
  tags: tags
}

resource logAnalytics 'Microsoft.OperationalInsights/workspaces@2025-07-01' existing = {
  name: names.logAnalytics
  scope: resourceGroup(names.rgManagement)
}

resource spokeVnet 'Microsoft.Network/virtualNetworks@2025-05-01' existing = {
  name: names.spokeVnet
  scope: resourceGroup(names.rgNetwork)
  resource privateEndpointSubnet 'subnets' existing = {
    name: subnetNames.privateEndpoints
  }
}

resource postgresDnsZone 'Microsoft.Network/privateDnsZones@2024-06-01' existing = {
  name: privateDnsZoneNames.postgres
  scope: resourceGroup(names.rgNetwork)
}

module postgres 'br/public:avm/res/db-for-postgre-sql/flexible-server:0.16.1' = {
  scope: rg
  params: {
    name: names.postgres
    location: location
    tags: tags
    version: settings.?version ?? '18'
    skuName: config.skuName
    tier: config.tier
    // With zone-redundant HA the primary and standby go to fixed, different zones.
    availabilityZone: zoneRedundant ? 1 : -1
    highAvailability: config.highAvailability
    highAvailabilityZone: zoneRedundant ? 2 : -1
    storageSizeGB: config.storageSizeGB
    autoGrow: 'Enabled'
    backupRetentionDays: config.backupRetentionDays
    geoRedundantBackup: config.geoRedundantBackup ? 'Enabled' : 'Disabled'
    authConfig: {
      activeDirectoryAuth: 'Enabled'
      passwordAuth: 'Disabled'
      tenantId: tenant().tenantId
    }
    // PostgreSQL names the admin role after the group's display name.
    administrators: empty(adminsGroupObjectId)
      ? []
      : [
          {
            objectId: adminsGroupObjectId
            principalName: spec.admins.group
            principalType: 'Group'
            tenantId: tenant().tenantId
          }
        ]
    publicNetworkAccess: 'Disabled'
    privateEndpoints: [privateEndpoint(spokeVnet::privateEndpointSubnet.id, postgresDnsZone.id, tags)]
    databases: [
      for db in settings.?databases ?? []: {
        name: db
        charset: 'UTF8'
        collation: 'en_US.utf8'
      }
    ]
    maintenanceWindow: {
      customWindow: 'Enabled'
      dayOfWeek: maintenanceWindow.dayOfWeek
      startHour: maintenanceWindow.startHour
      startMinute: maintenanceWindow.startMinute
    }
    enableAdvancedThreatProtection: threatProtection
    serverThreatProtection: threatProtection ? 'Enabled' : 'Disabled'
    diagnosticSettings: [{ workspaceResourceId: logAnalytics.id }]
  }
}

output postgresServerName string = postgres.outputs.name
output postgresFqdn string = postgres.outputs.?fqdn ?? ''
