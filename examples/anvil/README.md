# anvil

The bluepave example app: a small job-processing service in three parts that uses all three
platform APIs. Submit a text job in the UI; a worker processes it; the result is cached and
archived.

```
browser ──► web (Go, static UI) ──/api──► api (Go, REST) ──► PostgreSQL (AppDatabase)
                                              │  ▲                ▲
                                     publish  │  │ cache          │ claim jobs (FOR UPDATE SKIP LOCKED)
                                              ▼  │                │
                                          Valkey (AppCache) ──wake-up──► worker (Go) ──► blob (AppStorage)
```

| Component | Does | Uses |
|---|---|---|
| `web` | Serves the UI and forwards `/api/` to the API, so the browser sees one origin | api |
| `api` | Validates and stores jobs; serves them; caches finished jobs | PostgreSQL, cache |
| `worker` | Claims queued jobs from PostgreSQL, processes them, writes results, archives them | PostgreSQL, cache, blob storage |

**PostgreSQL is the queue;** the cache only carries wake-ups and cached reads. Losing the cache
(an in-cluster Valkey keeps nothing across restarts) slows things down but never loses a job. A
worker killed mid-job, for example by a Spot eviction, leaves the job `running`; another worker
requeues it after 2 minutes.

**No passwords exist.** The chart requests its backing services from the platform
(`deploy/chart/templates/platform-apis.yaml`): `AppDatabase` (`jobs`), `AppCache` (`cache`) and
`AppStorage` (`archive`). Each one creates an identity trusted by the service account `api`, and a
ConfigMap with the connection settings the pods read. Each new database connection gets a fresh
Entra token for `DATABASE_CLIENT_ID` as its password.

## Add it to your platform

In your platform repository (after `bluepave up`):

```bash
hack/add-example.sh anvil      # services/anvil/, apps/anvil.yaml, .github/workflows/anvil.yml
git checkout -b add-anvil && git add services/anvil apps/anvil.yaml .github/workflows/anvil.yml
git commit -m "Add the anvil example" && git push -u origin add-anvil   # and open a pull request
```

The script fills in your GitHub owner, repository and domain from `.bluepave/platform-settings.yaml`.
After the merge:
1. The golden path builds, scans and signs `anvil-web`, `anvil-api` and `anvil-worker`.
2. App onboarding creates `anvil-dev` and `anvil-staging`; Kargo deploys the images to dev.
3. Open `https://anvil.<env>.<domain>`. Promote to staging in Kargo.

It needs the `self-service` module with `data-postgres` (AppDatabase). AppStorage is off on the
`production` profile; the worker then just doesn't archive (`archive.enabled: false`).

## Settings (`deploy/chart/values.yaml`)

| Value | Default | |
|---|---|---|
| `cache.enabled`, `archive.enabled` | `true` | Request an AppCache / AppStorage |
| `rollout.enabled` | `true` | The api as an Argo Rollouts canary (`rollouts` module) |
| `rollout.analysis` | `false` | Smoke-test the canary first. Its pod runs curl: add `rollout.analysisImage` to policy-kyverno's `allowedImages` |
| `faultInjectionRate` | `"0"` | Share of API requests that fail on purpose, to watch canary analysis roll a release back |
| `route.hostname` | per stage | Public URL on the platform's Gateway |

## Locally

```bash
go test -race ./...
# PostgreSQL with a password, no cache or archive:
DATABASE_AUTH=password PGHOST=localhost PGUSER=postgres PGPASSWORD=... PGDATABASE=anvil go run ./cmd/api
```
