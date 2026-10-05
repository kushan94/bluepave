# runtime-falco

[Falco](https://falco.org) watches system calls on the nodes and logs an alert when a container
does something it shouldn't. Examples: a shell spawned in a container, reads of sensitive files,
or a package manager or network tool run inside a running container. It complements the
admission policies: those decide what may start, and Falco watches what it then does.

- **Modern eBPF probe:** no kernel module, so it works on AKS's Azure Linux nodes as they are.
- **Alerts are JSON on stdout** (priority `notice` and above): `kubectl logs -n falco ds/falco`.
  They stay in the pod logs; shipping them to a SIEM is up to the adopter.
- **Placement:** where the profile is highly available, Falco runs on every node, whatever its
  taints. On `trial`, it runs on the user nodes only (all apps run there), because the single
  system node has no CPU to spare.
- **Container metadata** comes from containerd on each node; the separate Kubernetes metadata
  collector isn't installed.

To turn it off (for example on a cluster where another runtime security tool runs):

```yaml
# bluepave.yaml
spec:
  modules:
    runtime-falco: { enabled: false }
```

## Runbook

- **Try it:** `kubectl exec -it <pod> -- sh` in an app namespace (on a debug image), then check the
  Falco logs for "Terminal shell in container".
- **Noisy rule:** prefer fixing the workload. Otherwise add a rule override through a module
  change, so every adopter gets the same rules.
