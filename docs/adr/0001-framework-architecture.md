# ADR-0001: bluepave is a framework of modules, driven by one versioned config API

- **Status:** Accepted
- **Date:** 2026-10-05

## Context

bluepave packages a working internal developer platform (Skyforge, the reference
implementation) so that anyone can install it on their own Azure subscription. A copy of
Skyforge with the IDs replaced would work once, for people who want exactly Skyforge. A
framework has to do more:
- let adopters choose capabilities;
- let others add capabilities without forking;
- upgrade safely;
- prove that every combination it claims to support works.

## Decisions

### 1. Four core concepts

| Concept | What it is | Contract |
|---|---|---|
| **Platform config** | `bluepave.yaml`: the adopter's choices | `api/v1alpha1/platform.schema.json` (JSON Schema), validated by the CLI and in CI |
| **Profile** | A named preset of module settings and sizing (`trial`, `standard`, `production`) | `profiles/<name>.yaml`; adopters override any value in `bluepave.yaml` |
| **Module** | One capability (AKS, Argo CD, Kyverno policies, AppStorage, the portal, ...) | `modules/<name>/module.yaml` (manifest) + its layers |
| **App contract** | What an app provides, and how it's onboarded | `api/v1alpha1/app.schema.json` (onboarding file) + the golden-path workflow inputs |

### 2. A module is self-contained and declares what it contributes

```yaml
# modules/policy-kyverno/module.yaml
apiVersion: bluepave.dev/v1alpha1
kind: Module
metadata:
  name: policy-kyverno
  description: Admission control: only images signed by the golden path
spec:
  version: 0.1.0
  requires: [aks, gitops-argocd]        # other modules
  config:                               # this module's settings, JSON Schema
    schema: config.schema.json
  layers:
    infra: infra/main.bicep             # optional: Azure resources (a deployment stack)
    gitops: gitops/                     # optional: Helm chart or add-on definition
    policies: policies/                 # optional: admission policies (with tests)
    portal: portal/                     # optional: templates, catalog entries
    docs: docs/                         # required: what it does, settings, runbook
  tests: tests/                         # required: layer tests run in CI
```

**Rules:**
- **Modules are independent.** They communicate only through declared `requires` and published
  outputs (for example, `aks` publishes the cluster's OIDC issuer). They never read each
  other's files.
- **The core is small:** the config API, the CLI, the module loader, and the root GitOps chart
  that turns enabled modules into Argo CD Applications.
- **Third parties add modules without forking:** the config lists extra module sources.

### 3. Layers keep their native tools

- **Bicep** for Azure. Each module with an `infra` layer is one deployment stack. Shared naming
  and config come from `bluepave.yaml` through `loadYamlContent`.
- **Helm and Argo CD** for the cluster. One root Application renders a chart that creates an
  Application per enabled module, with values from `bluepave.yaml` and the discovered IDs.
- **Kyverno and ValidatingAdmissionPolicy** for policies, with Kyverno CLI tests.
- **Backstage** for the portal; templates are module contributions.

The CLI orchestrates these tools. It never replaces them: no custom templating language, and no
hidden state.

### 4. The CLI is a single Go binary

`bluepave validate | plan | up | status | down | modules`.
- **Why Go:** one static binary per OS (no runtime to install), the Kubernetes ecosystem's
  language, and the same language as the reference app.
- **Dependencies:** the standard library plus a YAML parser and a JSON Schema validator.
- **Idempotent:** `up` converges and `down` asks first.
- **No hidden state:** state lives in Azure, Git, and `.bluepave/discovered.yaml` (IDs, never
  secrets).

### 5. Versioning and compatibility

- **The config API** is versioned (`apiVersion: bluepave.dev/v1alpha1`). Breaking changes bump the
  version, and the CLI migrates old files (`bluepave migrate`).
- **Releases** are SemVer. A release tags the repository, the golden-path workflow
  (`build-app.yml@v1`), the charts, the portal image and the CLI.
- **The admission policy trusts the golden path by tag** (`refs/tags/v<major>.*`), so a
  release signs images that clusters on the same major version accept.

### 6. Quality bar from the first commit

Every PR runs:
- schema validation of examples and profiles;
- CLI unit tests;
- module tests: Bicep build and PSRule, Helm render and unit tests, Kyverno CLI policy tests,
  and template dry runs.

A nightly end-to-end test runs `up` on a fresh subscription, deploys the examples, checks them,
and runs `down`. No profile or module is advertised without passing it.

### 7. Repository is a template

Adopters use "Use this template", edit `bluepave.yaml`, and run `bluepave up`. Upgrades are
merges from upstream releases. A product repository plus instance repositories pinned to
releases is the later step, once the module interfaces have settled.

## Consequences

- More structure than Skyforge has. Every capability is ported into a module with a manifest,
  config schema, docs and tests. That's the cost of letting people choose and extend.
- Skyforge stays the reference instance. Changes are proven there or in the end-to-end test
  before they're released.
- The module interface is `v1alpha1`. Expect it to change until v1.
