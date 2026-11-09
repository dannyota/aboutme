# Go and database rules

Read this file for Go, store, schema Go, or migration work.

For backend client-IP handling, read the authoritative [proxy rule](deploy.md) in deployment rules.

- Follow Google Go style with `gofmt`/`goimports`.
- Goose SQL in `apps/server/migrations/` is the migration source and sqlc reads it. Run `make sqlc-check`. `sqlc.yaml` keeps its `public.citext` override.
- One root module, `danny.vn/aboutme` (`go.mod`), holds `apps/server`, `packages/schema/gen/go`, and `deploy/observer`; run `go mod tidy` at the root. `deploy/caddy/production/build` stays a separate module.
- Run server Go commands under `apps/server`, and generated-schema Go checks under `packages/schema/gen/go`.
- Live DB targets use `-count=1`. Compose PostgreSQL is not host-published.
- golangci-lint uses `//nolint:`. A justified exception needs a reason. CI enforces errcheck, shadow, and US-spelling misspell.
- Go's `filepath.Glob` negates a class with `[^x]`, not `[!x]`.
- Before application `v1.0.0`, migration history may be edited, deleted, or collapsed. A trusted base containing root `.v1-baseline` freezes existing migrations; use forward corrective migrations after that boundary. Repository edits do not change already-applied database history; reconcile stored state explicitly during deployment. Grant `aboutme_app` explicitly ([ADR 0005](../docs/adr/0005-database-migrations.md)).

## Code rules

Apply these to authored Go you add, move, or touch; do not sweep unrelated code. Generated files follow their generator. Where Effective Go is silent, newer first-party guides apply.

- Give every exported symbol a doc comment, including struct fields, methods, and each constant; state the contract and units. Per-field comments are a stricter local rule. ([Google doc comments](https://google.github.io/styleguide/go/decisions#doc-comments), [Effective Go commentary](https://go.dev/doc/effective_go#commentary))
- Add operation context with `%w` when callers should inspect the cause, and test causes with `errors.Is` or `errors.As`. Wrap once at the useful boundary. Keep closed boundary errors and redaction; never expose token, provider, or SQL text. ([Google error wrapping](https://google.github.io/styleguide/go/best-practices#error-wrapping), [Effective Go errors](https://go.dev/doc/effective_go#errors), [Go wrapping guidance](https://go.dev/blog/go1.13-errors#whether-to-wrap))
- Pass `context.Context` first to work that accepts it and propagate request cancellation; framework handlers use the request context. A detached lifecycle context keeps its documented reason. ([Google contexts](https://google.github.io/styleguide/go/decisions#contexts), [context package](https://pkg.go.dev/context))
- Keep no package-level mutable runtime state. Inject clients, clocks, entropy, caches, and locks; use instance state, fresh slices, or switches. Only immutable sentinel errors, compiled regular expressions, and embedded assets may live at package level; never mutate or reassign them, and preserve error identity. ([Google global state](https://google.github.io/styleguide/go/best-practices#global-state))
- Define small interfaces at the consumer, only for an actual dependency, and normally return concrete types; an interface return is allowed for encapsulation, such as transaction facets. ([Google interfaces](https://google.github.io/styleguide/go/decisions#interfaces), [Effective Go interfaces](https://go.dev/doc/effective_go#interfaces))
- Prefer named table-driven subtests for repeated cases; subtests are optional. Keep distinct scenarios and concurrency tests separate. Do not rewrite tests wholesale. ([Google table-driven tests](https://google.github.io/styleguide/go/decisions#table-driven-tests), [Go table tests](https://go.dev/wiki/TableDrivenTests))
- `cmd/*/main.go` parses, wires, and owns lifecycle only; constructors live under `internal/`. This is a local architecture rule. ([Google least mechanism](https://google.github.io/styleguide/go/guide#least-mechanism), [Go server layout](https://go.dev/doc/modules/layout#server-project))
- Split an oversized file by concern into sibling files in the same package. File limits live in the [engineering standard](../docs/standards/engineering.md#links-and-size).

Verification gates live in [testing](testing.md). CI lifecycle and generated-file rules live in [verification](verification.md). Local execution follows [resources](resources.md).
