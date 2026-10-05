// The public DNS zone for the platform's domain, and one subzone per environment
// (<env>.<domain>), delegated from it. Certificate and DNS controllers get rights on their
// environment's subzone only, so an environment can only ever change its own records.
//
// The domain is registered elsewhere: set the `nameServers` output as its name servers at the
// registrar (`bluepave up` prints them).
targetScope = 'subscription'

import { dnsDomain, environmentNames, location, resourceNames, tagsFor } from '../../../lib/bicep/platform.bicep'

var names = resourceNames('global', subscription().subscriptionId)
var tags = tagsFor('global')

resource rg 'Microsoft.Resources/resourceGroups@2025-04-01' = {
  name: names.rgGlobal
  location: location
  tags: tags
}

module zone 'br/public:avm/res/network/dns-zone:0.6.2' = {
  scope: rg
  params: {
    name: dnsDomain
    tags: tags
    // Only Let's Encrypt may issue certificates for the domain and its subdomains: cheap
    // protection against mis-issuance.
    caa: [
      {
        name: '@'
        ttl: 3600
        caaRecords: [
          { flags: 0, tag: 'issue', value: 'letsencrypt.org' }
          { flags: 0, tag: 'issuewild', value: 'letsencrypt.org' }
        ]
      }
    ]
  }
}

module envZones 'br/public:avm/res/network/dns-zone:0.6.2' = [
  for env in environmentNames: {
    scope: rg
    name: 'zone-${env}'
    params: {
      name: '${env}.${dnsDomain}'
      tags: union(tags, { env: env })
    }
  }
]

module delegations 'delegation.bicep' = [
  for (env, i) in environmentNames: {
    scope: rg
    name: 'delegate-${env}'
    params: {
      parentZoneName: dnsDomain
      childLabel: env
      nameServers: envZones[i].outputs.nameServers
    }
    dependsOn: [zone]
  }
]

@description('Set these as the domain\'s name servers at the registrar.')
output nameServers array = zone.outputs.nameServers

@description('Each environment\'s subzone: { env, zone, resourceId }.')
output environmentZones array = [
  for (env, i) in environmentNames: {
    env: env
    zone: envZones[i].outputs.name
    resourceId: envZones[i].outputs.resourceId
  }
]
