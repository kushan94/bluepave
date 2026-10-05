# ADR-0002: The gitops layer: a root chart, one Application per module, and a module context

- **Status:** Accepted
- **Date:** 2026-10-05

## Context

ADR-0001 gives each module an optional `gitops` layer, rendered by Argo CD from one root
Application. Three things need deciding:
- how the cluster learns which modules are enabled. That depends on profiles, overrides and
  requirements, which Helm can't resolve;
- how a module gets environment-specific values (domain, IDs, profile) without each module
  re-deriving them;
- how module-specific mapping (e.g. "Entra client ID → Argo CD's `oidc.config`") stays inside
  the module, without a custom templating language.

The reference instance (Skyforge) used an ApplicationSet over add-on folders, per-environment
values files, and `values.generated.yaml` files written by bootstrap scripts.

## Decisions

### 1. The CLI resolves; Helm renders

`bluepave render` writes `.bluepave/resolved.yaml`:
- the active profile's spec;
- the enabled modules in install order, each with its gitops chart path, the repositories it
  installs from (`module.yaml` `spec.sources`), and its settings from `bluepave.yaml`.

The file is committed, like a lock file. `bluepave render -check` (run in CI) fails when it's out
of date. Its content sits under the top-level key `resolved`, and `.bluepave/discovered.yaml`'s
under `discovered`, so neither collides with `bluepave.yaml` when all three are Helm values
files.

### 2. The root chart (`platform/chart`)

The root Application, created by `bluepave up` for each environment, renders `platform/chart`
with three values files, `bluepave.yaml`, `resolved.yaml` and `discovered.yaml`, plus
`environment`. The chart creates:
- the `platform` AppProject: its sourceRepos are the platform repository plus every enabled
  module's `sources`;
- an Argo CD repository entry for each OCI Helm registry among those sources;
- one Application per enabled module with a gitops layer, `module-<name>`:
  - it points at `modules/<name>/gitops`;
  - its sync wave follows the install order;
  - it has no deletion finalizer, so removing a module from `bluepave.yaml` never uninstalls
    what the module created.

### 3. A module's gitops layer is a Helm chart that receives the module context

Each module chart gets two values:
- `bluepave`: the **module context**, computed once by the root chart;
- `settings`: the module's settings.

The chart renders whatever the module needs: usually an Application for an upstream chart, with
`valuesObject` computed from the context, plus plain manifests (routes, policies, stores). All
mapping is Helm templates inside the module. There are no generated values files.

The module context is part of the v1alpha1 module API. Fields may be added but not renamed or
removed:

| Field | Value |
|---|---|
| `platform`, `environment`, `prefix`, `region` | From `bluepave.yaml` and the root Application |
| `domain`, `apexDomain` | `<env>.<dns.domain>` and `<dns.domain>` |
| `github.owner`, `github.platformRepo` | From `bluepave.yaml` |
| `gitops.repoURL`, `gitops.revision` | Where Argo CD reads the platform repository |
| `tenantId`, `subscriptionId`, `admins.group`, `admins.groupObjectId` | Discovered (empty before `bluepave up`) |
| `profile` | The profile's spec, plus `name` |
| `modules` | Names of the enabled modules, to integrate with another module without reading its files |
| `discovered` | This environment's IDs: `discovered.environments.<env>` |
| `scheduling.tolerations` | Tolerations for platform pods (Spot, when the profile has Spot pools) |
| `scheduling.highAvailability` | Whether platform controllers run more than one replica: false on a cluster tier with no uptime SLA (`Free`, the trial profile) |

### 4. Discovered IDs

`.bluepave/discovered.yaml` layout:
- `discovered.azure.tenantId`, `discovered.azure.subscriptionId`;
- `discovered.admins.groupObjectId`;
- `discovered.ci.principalId`;
- `discovered.environments.<env>.<module>.<name>`: a module's infra outputs (e.g.
  `environments.dev.keyvault.keyVaultUri`) and IDs the CLI creates for it (e.g.
  `environments.dev.gitops-argocd.clientId`). A module may read another module's entry only if it
  requires that module, or checks that it's enabled.

A module renders without its IDs (degraded, e.g. no SSO) so the first sync works before
`bluepave up` has written them.

### 5. Tests

`hack/test-gitops.sh` (CI job `gitops`) runs, for every profile:
1. `bluepave render`;
2. the root chart;
3. each module chart, with the exact values the root gave it;
4. each upstream chart, with the values the module computed.

Every manifest is checked with kubeconform against Kubernetes and CRD schemas. A test fixture
of discovered IDs makes the ID-dependent paths render.

## Consequences

- **A forgotten `bluepave render` fails CI,** not the cluster.
- **Modules stay independent.** The only cross-module knowledge is the `modules` list in the
  context.
- **The context is a public interface:** changing it is an API change.
- **Wrapper charts aren't used.** A module installs its upstream chart through an Application
  rather than a Helm dependency. That keeps each add-on a separate Argo CD app (its own health,
  sync and history), as in the reference instance.
