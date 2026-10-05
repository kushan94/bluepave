// Makes the AppDatabase admin identity an Entra administrator of the platform's PostgreSQL server
// (the resource group this module is deployed to, data-postgres's). A child resource of its own:
// the data-postgres stack keeps managing the server and its other administrators.

@description('The PostgreSQL Flexible Server (data-postgres).')
param serverName string

@description('Object ID of the identity.')
param principalId string

@description('Its name; PostgreSQL names the admin role after it.')
param principalName string

resource server 'Microsoft.DBforPostgreSQL/flexibleServers@2024-08-01' existing = {
  name: serverName
}

resource admin 'Microsoft.DBforPostgreSQL/flexibleServers/administrators@2024-08-01' = {
  parent: server
  name: principalId
  properties: {
    principalName: principalName
    principalType: 'ServicePrincipal'
    tenantId: tenant().tenantId
  }
}
