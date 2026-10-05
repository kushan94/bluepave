// Azure Monitor workspace: the managed Prometheus metrics store. There is no AVM module for it yet.

@description('Workspace name.')
param name string

@description('Azure region.')
param location string

@description('Resource tags.')
param tags object = {}

resource workspace 'Microsoft.Monitor/accounts@2025-10-03' = {
  name: name
  location: location
  tags: tags
}

output resourceId string = workspace.id
