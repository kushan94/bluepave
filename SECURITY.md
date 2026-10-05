# Security policy

## Reporting a vulnerability

Please **don't open a public issue.** Use GitHub's private vulnerability reporting (the
repository's **Security** tab → **Report a vulnerability**). You'll get an answer within a week.
Fixes are released as patch versions, with an advisory.

## Supported versions

Only the latest minor release gets fixes before v1.0.

## What bluepave assumes

- The installer is an Owner of the target Azure subscription and can create Entra ID app
  registrations.
- Workload images are trusted only when signed by the platform's golden-path workflow, from a
  release tag. Branches can't publish (GitHub environments limited to the default branch).
- The platform's GitHub App can write to the repositories it's installed on. Install it on the
  platform and app repositories only.

The design decisions behind each control are in [docs/adr](docs/adr).
