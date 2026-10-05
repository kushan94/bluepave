# policy-kyverno

Admission control for app namespaces, the namespaces app onboarding labels
`platform.bluepave.dev/app: <app>`. Two policies run there:

| Policy | Engine | Rule |
|---|---|---|
| `verify-app-images` | Kyverno `ImageValidatingPolicy` | Every image from the platform registry carries a Cosign keyless signature made by the platform's golden path: `https://github.com/<owner>/<platformRepo>/.github/workflows/build-app.yml` at `main` or a release tag, issued by GitHub's OIDC issuer and recorded in Rekor |
| `app-allowed-images` | Kubernetes `ValidatingAdmissionPolicy` (CEL, run by the API server) | Pods run only `<registry>/<app>-*` images (their own app's), plus `settings.allowedImages` |

Together they mean an app can only run its own images, built and signed by the golden path. An
image built anywhere else, or another app's image, is refused before it starts. Per-app
repository permissions in the registry enforce the same split on the push side.

- **The golden path signs, not the app:** the signing certificate names `build-app.yml` in the
  platform repository, whichever app repository called it. So one trusted identity covers every
  app, and apps can't sign their own images.
- **Kyverno reads signatures with Workload Identity**
  (`id-<prefix>-<env>-<region>-kyverno`, federated to the admission and reports controllers),
  with pull rights on the registry only.
- **Fail closed:** `failurePolicy: Fail`, so app pods aren't admitted while Kyverno is down.
  Highly available profiles run three admission replicas on system nodes. `trial` runs one, which
  may land on Spot; an eviction blocks new app pods for a few minutes.
- **Reports:** background scans write PolicyReports (`kubectl get policyreport -A`), which is
  how `Audit` mode shows what would be refused.
- The policies appear once `bluepave up` has recorded the registry's login server.

## Settings

```yaml
# bluepave.yaml
spec:
  modules:
    policy-kyverno:
      settings:
        signatureAction: Audit          # default Deny; Audit while moving apps onto the golden path
        signerRefRegExp: refs/heads/main   # default: main and release tags
        allowedImages:                  # exact references app pods may also run
          - docker.io/curlimages/curl:8.22.0
```

**Outputs:** `clientId`.

## Runbook

- **Pod refused, "must be signed":** check how the image was built with
  `cosign verify <image> --certificate-identity-regexp ... --certificate-oidc-issuer https://token.actions.githubusercontent.com`.
  Only images built by `build-app.yml` from `main` or a release tag pass.
- **Pod refused, "images must come from":** the app may only run `<registry>/<app>-*`. Add
  third-party helper images to `allowedImages`, pinned by tag or digest.
- **Policy changes:** test them with `hack/test-policies.sh` and roll them out in `Audit` first.
  In the reference instance an untested signer change once blocked every app until it was
  reverted.
