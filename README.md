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
  - platform APIs such as `AppStorage` (a keyless blob store from five lines of YAML).
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

## License

To be decided before the first release.
