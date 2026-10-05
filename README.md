# bluepave

**An Azure-native internal developer platform: a paved road from a repository to a signed,
promoted, running app on AKS.**

> Status: **pre-release.** bluepave is being extracted from a working platform (Skyforge) into
> something anyone can install on their own Azure subscription. See
> [docs/design/configuration.md](docs/design/configuration.md) for the plan.

## What you get

- **Infrastructure as code (Bicep, deployment stacks):** landing zone, AKS (Azure CNI Overlay +
  Cilium, Entra ID only, Workload Identity), ACR, Key Vault, PostgreSQL, DNS. No passwords
  anywhere.
- **A golden-path pipeline** that any app repository calls with ten lines of YAML:
  - checks for Go and Python apps, plus Semgrep, gitleaks and Trivy;
  - an image with an SBOM and SLSA provenance;
  - a Cosign keyless signature.
- **Guardrails at admission:**
  - only images signed by the golden path;
  - each app runs only its own images (per-app registry permissions, enforced in Azure);
  - Pod Security `restricted`, default-deny networking, Falco.
- **Delivery:** Argo CD (GitOps) and Kargo (promotion dev → staging with verification), plus Argo
  Rollouts canaries.
- **Self-service:**
  - one onboarding file per app (namespaces, Argo CD project, generated Kargo pipeline);
  - platform APIs: `AppStorage`, `AppCache` and `AppDatabase`, each keyless and a few lines of
    YAML, backed per profile (in-cluster or Azure-managed).
- **A developer portal (Backstage):**
  - a catalog, TechDocs, and a live Kubernetes view;
  - templates for new Go and Python services;
  - Microsoft Entra ID sign-in.
- **Observability:** OpenTelemetry → Tempo, managed Prometheus, Grafana, SLO burn-rate alerts.

## Profiles

| Profile | For | Approx. cost |
|---|---|---|
| `trial` | Learning, demos: fits an Azure Free Trial (4 + 3 vCPUs) | US$3–5/day |
| `standard` | A team's dev/test platform | tbd |
| `production` | Private cluster, Azure Firewall, private endpoints, zone redundancy | tbd |

## Getting started (in progress)

```bash
go run ./cmd/bluepave validate                   # check the configuration, module settings and apps/*.yaml
go run ./cmd/bluepave modules                    # list available modules
go run ./cmd/bluepave render                     # resolve modules for GitOps (.bluepave/resolved.yaml)
go run ./cmd/bluepave plan [-what-if]            # the deployment stacks, in order (and their Azure changes)
go run ./cmd/bluepave up                         # accounts, infra, identities (as a subscription Owner)
```

`up` runs as you (`az login` as a subscription Owner, `gh auth login` with admin on the platform
repository):
1. **accounts:** resource providers, the admins group (you're added), the CI identity trusted by
   the platform repository's GitHub environments (main only), repository variables.
2. **infra:** every module's deployment stack, in order, outputs into `.bluepave/discovered.yaml`.
3. **identities:** Entra apps for Argo CD, Grafana and the portal, Kargo's admin credentials and
   the portal's client secret in Key Vault.

The platform GitHub App and the GitOps bootstrap (Argo CD) are next
([docs/design/configuration.md](docs/design/configuration.md), section 8).

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md), [SECURITY.md](SECURITY.md), and the architecture in
[docs/adr/0001-framework-architecture.md](docs/adr/0001-framework-architecture.md).

## License

[Apache License 2.0](LICENSE).
