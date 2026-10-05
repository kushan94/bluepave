# edge-gateway

The platform's public entry point. Every app and platform UI is served at `https://<name>.<env>.<domain>`.

- **[Envoy Gateway](https://gateway.envoyproxy.io)** implements the Kubernetes Gateway API. One
  `Gateway`, `public` in namespace `gateway`, sits behind the AKS public load balancer.
  - `:80` redirects to HTTPS.
  - `:443` terminates TLS with one wildcard certificate, `*.<env>.<domain>`, from Let's Encrypt
    (DNS-01 through the `certificates` module).
- **Apps** attach `HTTPRoute`s from their namespaces (labelled `platform.bluepave.dev/app` by app
  onboarding) to the wildcard listener `https`.
- **Platform UIs** (Argo CD, Grafana, ...) get their own listener `https-<name>`, which only their
  namespace may use. A module declares its hostnames in `module.yaml`, and `bluepave validate`
  rejects two modules claiming the same one:

  ```yaml
  spec:
    hostnames:
      - { name: argocd, namespace: argocd }
  ```

  The listener with the most specific hostname wins, so an app route claiming
  `argocd.<env>.<domain>` on the wildcard listener never receives its traffic.
- **[external-dns](https://kubernetes-sigs.github.io/external-dns)** creates DNS records for route
  hostnames in `<env>.<domain>`. It signs in with Workload Identity
  (`id-<prefix>-<env>-<region>-external-dns`), whose role is limited to that zone. It owns its
  records through TXT markers and never deletes others'.
- **Replicas:** two Envoy proxies where the profile is highly available, one on `trial`.

**Outputs:** `clientId`, `dnsZoneName`, `dnsZoneResourceGroup`.

## Limitations

- **Firewall egress (`network.egress: firewall`):** traffic from the internet enters through the
  AKS load balancer, but replies leave through the firewall's route. That asymmetric path breaks
  connections. The fix is Application Gateway for Containers (profile `ingress: agc`) or a
  firewall DNAT rule. Neither is implemented yet, so the `production` profile uses `inCluster`,
  and firewall egress with a public Gateway needs that work first.

## Runbook

- **Certificate:** run `kubectl -n gateway get certificate wildcard`. Not ready usually means the
  DNS zone isn't delegated yet (see the `certificates` runbook).
- **No DNS record for a route:** check `kubectl -n external-dns logs deploy/external-dns`, and
  that the route has `hostnames` and is `Accepted` by the Gateway
  (`kubectl get httproute -A`).
- **Route not accepted:** the namespace isn't allowed on that listener. Apps use the wildcard
  listener `https`. Platform hostnames must be declared in the module's `module.yaml`.
