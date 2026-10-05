# Tests

Covered by the standard layer checks run for every module in CI (Bicep build and lint of the
infra layer against the repository's bluepave.yaml and every profile). Module-specific tests go here.

Checked with an Azure what-if under the `trial` and `production` profiles before merging,
including the `databases`, `maintenanceWindow` and `threatProtection` settings.
