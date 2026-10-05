# ${{ values.name }}

${{ values.description }}

Created from the portal template "New Go service". It runs on the platform like any app:

| Path | What it is |
|---|---|
| `cmd/api/` | The service: image `${{ values.name }}-api`, built, scanned and signed by the golden path (`.github/workflows/${{ values.name }}.yml`) |
| `deploy/chart/` | Its Helm chart; `deploy/values-<stage>.yaml` per stage, where Kargo writes the image it promotes |
| `catalog-info.yaml` | Its entry in the portal's catalog |
| `apps/${{ values.name }}.yaml` (repository root) | Its onboarding: namespaces `${{ values.name }}-dev` and `${{ values.name }}-staging`, Argo CD project, Kargo pipeline |

New images go to dev automatically; staging is promoted by hand in Kargo.

```bash
go test ./...
go run ./cmd/api        # http://localhost:8080, metrics on :9090/metrics
```
