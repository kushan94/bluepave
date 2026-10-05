# dns

The platform's public DNS, deployed **once per subscription**:
- the zone for `spec.dns.domain`, which only lets Let's Encrypt issue certificates (CAA);
- one subzone per environment (`dev.<domain>`, `prod.<domain>`), delegated from it.

Apps get `<app>.<env>.<domain>`. external-dns writes the records, and cert-manager proves domain
ownership for the wildcard certificate. Both are limited to their environment's subzone.

**Delegating the domain:** register the domain anywhere, then set the zone's name servers (the
`nameServers` output, printed by `bluepave up`) at the registrar. Until that's done, certificates
can't be issued.

**Outputs:**
- `nameServers`
- `environmentZones`: a list of `{ env, zone, resourceId }`.
