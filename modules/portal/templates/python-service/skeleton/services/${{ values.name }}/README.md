# ${{ values.name }}

${{ values.description }}

Created from the portal template "New Python service". It runs on the platform like any app:

| Path | What it is |
|---|---|
| `app/` | The service (FastAPI): image `${{ values.name }}-web`, built, scanned and signed by the golden path (`.github/workflows/${{ values.name }}.yml`) |
| `deploy/chart/` | Its Helm chart; `deploy/values-<stage>.yaml` per stage, where Kargo writes the image it promotes |
| `catalog-info.yaml` | Its entry in the portal's catalog |
| `apps/${{ values.name }}.yaml` (repository root) | Its onboarding: namespaces `${{ values.name }}-dev` and `${{ values.name }}-staging`, Argo CD project, Kargo pipeline |

New images go to dev automatically; staging is promoted by hand in Kargo.

```bash
python -m venv .venv && . .venv/bin/activate
pip install --require-hashes -r requirements-dev.txt
pytest && ruff check .
uvicorn app.main:app --port 8080   # metrics on :9090/metrics
```

Dependencies are pinned with hashes. After editing `requirements*.in`, regenerate with
`uv pip compile requirements.in --generate-hashes --python-version 3.14 --python-platform linux -o requirements.txt`
(and the same for `requirements-dev.in`).
