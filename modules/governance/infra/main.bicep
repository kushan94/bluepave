// Subscription-wide guardrails, deployed once per subscription. Policy assignments need rights the
// pipeline doesn't have, so `bluepave up` deploys this module as the installer (an Owner).
targetScope = 'subscription'

import { budgetContactEmails, budgetFor, environmentNames, location, platformName, prefix } from '../../../lib/bicep/platform.bicep'

@description('First day of the month the budgets start (yyyy-MM-01). Budgets can\'t start in the past or change their start, so the first deployment picks this month and `bluepave up` passes the recorded output back from then on.')
param budgetStartDate string = '${utcNow('yyyy-MM')}-01'

@description('Tags that resources inherit from their resource group when missing (keeps cost reports by env complete).')
param inheritedTags string[] = ['env', 'platform']

// Shown in the portal as "Assigned by", so anyone can trace an assignment back to this module.
var assignedBy = 'bluepave ${platformName}: modules/governance'

// Built-in policy definitions (https://github.com/Azure/azure-policy).
var policyIds = {
  allowedLocations: '/providers/Microsoft.Authorization/policyDefinitions/e56962a6-4747-49cd-b67b-bf8b01975c4c'
  allowedRgLocations: '/providers/Microsoft.Authorization/policyDefinitions/e765b5de-1225-4ba3-bd56-1ac6695af988'
  requireRgTag: '/providers/Microsoft.Authorization/policyDefinitions/96670d01-0a4d-4649-9c89-2d3abc0a5025'
  inheritRgTag: '/providers/Microsoft.Authorization/policyDefinitions/ea3f2387-9b95-492a-a190-fcdc54f7b070'
}

// Tag Contributor: lets the inherit-tag policies' identities add missing tags during remediation.
var tagContributorRoleId = subscriptionResourceId(
  'Microsoft.Authorization/roleDefinitions',
  '4a9ae827-6dc8-4573-8ac7-8239d42aa03f'
)

resource allowedLocations 'Microsoft.Authorization/policyAssignments@2025-03-01' = {
  name: '${prefix}-allowed-locations'
  properties: {
    displayName: '${platformName}: allowed locations for resources'
    description: 'Deny resources outside the platform\'s region. Global resources (DNS zones) are exempt by the definition.'
    metadata: { assignedBy: assignedBy }
    policyDefinitionId: policyIds.allowedLocations
    parameters: { listOfAllowedLocations: { value: [location] } }
  }
}

resource allowedRgLocations 'Microsoft.Authorization/policyAssignments@2025-03-01' = {
  name: '${prefix}-allowed-rg-locations'
  properties: {
    displayName: '${platformName}: allowed locations for resource groups'
    description: 'Deny resource groups outside the platform\'s region.'
    metadata: { assignedBy: assignedBy }
    policyDefinitionId: policyIds.allowedRgLocations
    parameters: { listOfAllowedLocations: { value: [location] } }
  }
}

// Audit only: Azure creates some resource groups itself (NetworkWatcherRG, the AKS node resource
// group) without these tags, and Deny would break them. DoNotEnforce still reports them.
resource requireEnvTag 'Microsoft.Authorization/policyAssignments@2025-03-01' = {
  name: '${prefix}-require-rg-env-tag'
  properties: {
    displayName: '${platformName}: resource groups must have an env tag (audit)'
    description: 'Report resource groups without an env tag.'
    metadata: { assignedBy: assignedBy }
    policyDefinitionId: policyIds.requireRgTag
    enforcementMode: 'DoNotEnforce'
    parameters: { tagName: { value: 'env' } }
  }
}

resource inheritTags 'Microsoft.Authorization/policyAssignments@2025-03-01' = [
  for tag in inheritedTags: {
    name: '${prefix}-inherit-tag-${tag}'
    location: location
    identity: { type: 'SystemAssigned' }
    properties: {
      displayName: '${platformName}: inherit the ${tag} tag from the resource group'
      description: 'Copy the ${tag} tag from the resource group to resources that lack it, so cost reports by tag stay complete.'
      metadata: { assignedBy: assignedBy }
      policyDefinitionId: policyIds.inheritRgTag
      parameters: { tagName: { value: tag } }
    }
  }
]

resource inheritTagsRoles 'Microsoft.Authorization/roleAssignments@2022-04-01' = [
  for (tag, i) in inheritedTags: {
    name: guid(subscription().id, inheritTags[i].id, tagContributorRoleId)
    properties: {
      principalId: inheritTags[i].identity.principalId
      principalType: 'ServicePrincipal'
      roleDefinitionId: tagContributorRoleId
    }
  }
]

var thresholds = [
  { name: 'actual50', threshold: 50, type: 'Actual' }
  { name: 'actual80', threshold: 80, type: 'Actual' }
  { name: 'forecast100', threshold: 100, type: 'Forecasted' }
]

// One budget per environment, filtered on the env tag the policies above keep in place.
resource budgets 'Microsoft.Consumption/budgets@2026-06-01' = [
  for env in environmentNames: {
    name: 'budget-${prefix}-${env}'
    properties: {
      category: 'Cost'
      amount: budgetFor(env)
      timeGrain: 'Monthly'
      timePeriod: { startDate: budgetStartDate }
      filter: { tags: { name: 'env', operator: 'In', values: [env] } }
      notifications: toObject(thresholds, t => t.name, t => {
        enabled: true
        operator: 'GreaterThanOrEqualTo'
        threshold: t.threshold
        thresholdType: t.type
        contactRoles: ['Owner']
        contactEmails: budgetContactEmails
      })
    }
  }
]

@description('The budgets\' start date, passed back as the parameter on later deployments.')
output budgetStartDate string = budgetStartDate
