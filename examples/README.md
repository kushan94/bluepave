# Examples

Apps that show the platform end to end. They aren't part of the platform: add one to your
platform repository with `hack/add-example.sh <name>`, as a template from the portal would.

| Example | Shows |
|---|---|
| [anvil](anvil/) | Go, three images (web, api, worker), all three platform APIs (AppDatabase, AppCache, AppStorage), an Argo Rollouts canary, tracing |

`hack/test-examples.sh` adds each one to a copy of this repository's settings and checks it; CI
also runs the golden path on each (build, scan, no publish). The end-to-end test deploys them.
