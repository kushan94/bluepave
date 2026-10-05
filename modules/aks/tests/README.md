# Tests

Covered by the standard layer checks run for every module in CI (Bicep build and lint of the
infra layer against the repository's bluepave.yaml and every profile). Module-specific tests go here.

The cluster resource is also checked with an Azure what-if (preflight) under the `trial` and
`production` profiles before a change is merged.
