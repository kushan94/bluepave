# certificates

[cert-manager](https://cert-manager.io) issues and renews TLS certificates for the platform's
Gateway and anything else that asks. It comes with two Let's Encrypt `ClusterIssuer`s:

| Issuer | For |
|---|---|
| `letsencrypt` | Real, browser-trusted certificates (production rate limits) |
| `letsencrypt-staging` | Experiments: generous rate limits, not browser-trusted |

- **DNS-01 challenges:** cert-manager proves control of `<env>.<domain>` by writing a TXT record
  in the environment's Azure DNS zone (created by the `dns` module). This works for wildcard
  certificates and for clusters with no public HTTP endpoint.
- **No stored credentials:** it signs in with Workload Identity
  (`id-<prefix>-<env>-<region>-cert-manager`). That identity holds DNS Zone Contributor on
  `<env>.<domain>` only, so it can't touch the apex zone or another environment's zone.
- **CRDs are kept** when the chart is removed, so uninstalling can't delete every `Certificate`.
- The issuers appear once `bluepave up` has recorded this module's outputs.

Some charts need cert-manager for their own webhook certificates (Kargo, for example), so those
modules require this one.

## Settings

```yaml
# bluepave.yaml
spec:
  modules:
    certificates:
      settings:
        acmeEmail: platform-team@example.com   # Let's Encrypt expiry and policy notices
```

**Outputs:** `clientId`, `dnsZoneName`, `dnsZoneResourceGroup`.

## Runbook

- **Certificate not ready:** run `kubectl describe certificate <name> -n <ns>`, then look at the
  `CertificateRequest`, `Order` and `Challenge` it lists. A pending DNS-01 challenge usually means
  the zone isn't delegated yet: check the name servers at the registrar (`bluepave up` prints
  them).
- **Rate limited:** use `letsencrypt-staging` while experimenting.
