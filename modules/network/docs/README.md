# network

Per-environment hub-and-spoke network.

| Part | Details |
|---|---|
| Hub `vnet-<prefix>-<env>-<region>-hub` | `AzureFirewallSubnet` (the firewall exists only with egress `firewall`) |
| Spoke `vnet-<prefix>-<env>-<region>-spoke` | `snet-aks-nodes` (/22, node IPs; pods use the CNI Overlay range), `snet-aks-apiserver` (/28, delegated to AKS for API Server VNet Integration), `snet-agc` (/24, Application Gateway for Containers), `snet-private-endpoints` (/24) |
| NSGs | One per spoke subnet: SSH/RDP to other VNet hosts denied; HTTP/HTTPS from the internet allowed where ingress lands (the nodes for `inCluster`, the AGC subnet for `agc`) |
| Egress (`network.egress`) | `loadBalancer`: the AKS load balancer, nothing extra. `natGateway`: a NAT Gateway on the node subnet. `firewall`: zone-redundant Azure Firewall with an FQDN allow-list, plus a route table forcing all spoke egress through it. |
| Private DNS | Zones for ACR, Key Vault, Blob, PostgreSQL and Redis Private Link, linked to both VNets |

**Address spaces:** `bluepave.yaml` `spec.network.<env>` (defaults: dev hub 10.10.0.0/22 and spoke
10.11.0.0/16; prod 10.20.0.0/22 and 10.21.0.0/16). Subnets are carved from the spoke with
`cidrSubnet()`.

**Settings** (`spec.modules.network.settings`): `allowedEgressFqdns` replaces the firewall's
default allow-list of container registries and Git hosts.

**Requires:** `monitoring` (diagnostic settings go to its Log Analytics workspace).
