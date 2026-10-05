# governance

Subscription-wide guardrails, deployed **once per subscription** by the installer, who is an
Owner. The pipeline's identity can't change policy, by design.

| Resource | Does |
|---|---|
| `<prefix>-allowed-locations`, `<prefix>-allowed-rg-locations` | Deny resources and resource groups outside `spec.azure.region` (global resources such as DNS zones are exempt) |
| `<prefix>-require-rg-env-tag` | Audit resource groups without an `env` tag (audit only: Azure creates some groups itself) |
| `<prefix>-inherit-tag-env`, `-platform` | Copy those tags from resource groups to resources, so cost reports by tag are complete |
| `budget-<prefix>-<env>` | Monthly budget per environment: alerts at 50%, 80% and a 100% forecast to subscription Owners and `spec.budget.contactEmails` |

**Settings:**
- `spec.budget.monthly.<env>`: defaults to the profile's budget.
- `spec.budget.contactEmails`.
- `spec.modules.governance.settings.additionalLocations`: regions allowed besides
  `spec.azure.region`. The location policies cover the **whole subscription**, so set this when
  the subscription also runs something elsewhere (another platform, other workloads), or turn the
  module off there (`governance: {enabled: false}`) and keep the guardrails you already have.

**Parameter:** `budgetStartDate` (`yyyy-MM-01`). A budget can't start in the past, so
`bluepave up` records the first value and reuses it.
