# Contributing to bluepave

Thanks for helping. bluepave is a framework of modules behind a versioned config API
([ADR-0001](docs/adr/0001-framework-architecture.md)); most contributions are a new module or a
change to one.

## Ground rules

- **Every change comes with tests.** CI runs the CLI tests, validates every profile and example
  against the config API, and checks every module manifest. Modules add their own layer tests
  (Bicep build, Helm render, policy tests, template dry runs).
- **No account-specific values in code.** Anything that differs between adopters comes from
  `bluepave.yaml`, or is discovered by the CLI into `.bluepave/discovered.yaml`. Secrets never go
  in Git.
- **Decisions are written down.** A change to a contract (config API, module manifest, app
  contract, golden-path inputs) needs an ADR in `docs/adr/`.
- **Pin what you depend on:** actions by commit SHA, charts and modules by version, images by
  digest.

## Adding a module

1. Create `modules/<name>/module.yaml` (schema: `api/v1alpha1/module.schema.json`), with `docs/`
   and `tests/`.
2. Run `go run ./cmd/bluepave modules` and `go run ./cmd/bluepave validate -f examples/bluepave.yaml`.
3. Add the module to the profiles that should enable it by default.

## Development

```bash
go test ./...
go run ./cmd/bluepave validate -f examples/bluepave.yaml
```

Commits follow the existing style: an imperative subject saying what changed, and a body saying
why.
