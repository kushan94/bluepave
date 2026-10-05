// Compile-time check of the library against the repository's bluepave.yaml: `bicep build` must
// succeed, and the outputs show what modules will see.
targetScope = 'subscription'
import { adminsGroupObjectId, budgetFor, ciPrincipalId, environmentNames, location, networkFor, prefix, profile, regionCode, resourceNames, tagsFor } from 'platform.bicep'

output prefix string = prefix
output location string = location
output regionCode string = regionCode
output environments array = environmentNames
output devNetwork object = networkFor('dev')
output devBudget int = budgetFor('dev')
output clusterTier string = profile.cluster.tier
output names object = resourceNames('dev', '00000000-0000-0000-0000-000000000000')
output tags object = tagsFor('dev')
output adminsGroupObjectId string = adminsGroupObjectId
output ciPrincipalId string = ciPrincipalId
