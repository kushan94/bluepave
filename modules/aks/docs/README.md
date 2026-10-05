# aks

One AKS cluster per environment (`aks-<prefix>-<env>-<region>`), in the spoke the `network` module
created:

- **Networking:** Azure CNI Overlay with the Cilium dataplane and network policy. Pods use
  10.244.0.0/16 and services 10.0.0.0/16, so they never take VNet addresses. Nodes sit in
  `snet-aks-nodes`, and the API server in `snet-aks-apiserver` (API Server VNet Integration).
- **Access:** Entra ID only. Local accounts are disabled and Kubernetes authorization goes through
  Azure RBAC. The platform admins group gets Azure Kubernetes Service RBAC Cluster Admin and
  Cluster User once `bluepave up` has discovered it.
- **Workload Identity:** the OIDC issuer is on, so pods get Entra tokens through federated
  credentials. Modules that run add-ons create their own identities, trusting `oidcIssuerUrl`.
- **Images:** the kubelet pulls from the environment's registry with its managed identity
  (Container Registry Repository Reader with repository permissions, otherwise AcrPull). There
  are no pull secrets.
- **Monitoring:** control-plane logs (audit of changes, Entra sign-in, scheduler, controller
  manager, autoscaler) go to Log Analytics, and managed Prometheus metrics go to the Azure
  Monitor workspace.
- **Nodes:** Azure Linux on ephemeral OS disks across zones 1-3. The system pool is declared on the
  cluster. User pools are child resources: Spot pools have no upgrade settings (AKS rejects them),
  and regular pools surge or replace nodes in place.

| Profile setting | Effect |
|---|---|
| `cluster.tier` | `Free` (no SLA), `Standard` or `Premium` |
| `cluster.kubernetesVersion` | Minor version of the control plane and every pool |
| `cluster.autoUpgrade` | Patch and node-image upgrades in the maintenance window; off means you upgrade by hand |
| `cluster.systemPool`, `cluster.userPools` | Sizes, counts, Spot, and surge per pool |
| `cluster.privateCluster` | API server reachable only from the VNet |
| `cluster.advancedNetworking` | Advanced Container Networking Services (flow logs, FQDN policy) |
| `cluster.addons` | KEDA, VPA, Image Cleaner |
| `network.egress` | `loadBalancer`, `natGateway` or `firewall` (user-defined routing) |

## Settings

```yaml
# bluepave.yaml
spec:
  modules:
    aks:
      settings:
        maintenanceWindow:     # default: Sunday 02:00-06:00 UTC
          dayOfWeek: Sunday
          startTime: "02:00"
          durationHours: 4
          utcOffset: "+08:00"
```

**Outputs:** `clusterName`, `resourceGroupName`, `oidcIssuerUrl`, `kubeletIdentityObjectId`.

## Runbook

- **Credentials:** run `az aks get-credentials -g rg-<prefix>-<env>-<region>-aks -n aks-<prefix>-<env>-<region>`,
  then `kubelogin convert-kubeconfig -l azurecli`.
- **Manual upgrade** (`autoUpgrade: false`):
  1. Run `az aks upgrade --control-plane-only`, then upgrade each pool.
  2. On the trial profile, scale regular user pools to 0 first: a system-pool surge node doesn't
     fit the Free Trial quota next to them.
- **Spot evictions:** Spot nodes can go at any time. Keep anything the platform needs to admit
  pods (admission webhooks, the GitOps controller) on regular nodes.
