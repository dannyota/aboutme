# Go and database rules

Read this file for Go, store, schema Go, or migration work.

For backend client-IP handling, read the authoritative [proxy rule](deploy.md) in deployment rules.

- Follow Google Go style with `gofmt`/`goimports`.
- Goose SQL in `apps/server/migrations/` is the migration source and sqlc reads it. Run `make sqlc-check`. `sqlc.yaml` keeps its `public.citext` override.
- Run server Go commands under `apps/server`, and generated-schema Go checks under `packages/schema/gen/go`.
- Live DB targets use `-count=1`. Compose PostgreSQL is not host-published.
- golangci-lint uses `//nolint:`; Semgrep uses `// nosemgrep: <rule-id>`. A justified exception needs both, with reasons. CI enforces errcheck, shadow, and US-spelling misspell.
- Go's `filepath.Glob` negates a class with `[^x]`, not `[!x]`.
- Before application `v1.0.0`, migration history may be edited, deleted, or collapsed. A trusted base containing root `.v1-baseline` freezes existing migrations; use forward corrective migrations after that boundary. Repository edits do not change already-applied database history; reconcile stored state explicitly during deployment. Grant `aboutme_app` explicitly ([ADR 0005](../docs/adr/0005-database-migrations.md)).

Verification gates live in [testing](testing.md). CI lifecycle and generated-file rules live in [verification](verification.md). Local execution follows [resources](resources.md).
