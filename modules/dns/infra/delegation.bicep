// Delegates a child zone (e.g. dev.example.com) from its parent with an NS record set.

@description('Parent zone, e.g. example.com.')
param parentZoneName string

@description('Label of the child zone under the parent, e.g. dev.')
param childLabel string

@description('Name servers of the child zone.')
param nameServers string[]

resource parent 'Microsoft.Network/dnsZones@2018-05-01' existing = {
  name: parentZoneName
}

// 2018-05-01 is the newest non-preview API version for public DNS zones.
#disable-next-line use-recent-api-versions
resource delegation 'Microsoft.Network/dnsZones/NS@2018-05-01' = {
  parent: parent
  name: childLabel
  properties: {
    TTL: 3600
    NSRecords: [for ns in nameServers: { nsdname: ns }]
  }
}
