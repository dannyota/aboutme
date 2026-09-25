# Gotchas

Non-obvious facts about this repository's tools. Implementers, qa, and devops read this file.

- Use Podman and `podman compose`, never Docker.
- Goose SQL in `apps/server/migrations/` is the migration source and sqlc reads it. Run `make sqlc-check`. `sqlc.yaml` keeps its `public.citext` override.
- `.env` is ignored and holds credentials. Never print, document, or stage values; add names with empty values to `.env.example`.
- Caddy is the client-IP trust boundary. Go accepts the canonical header only from configured trusted proxies and never parses `X-Forwarded-For`.
- Run `make` at the repository root, server Go commands under `apps/server`, and generated-schema Go checks under `packages/schema/gen/go`.
- Live DB targets use `-count=1`. Compose PostgreSQL is not host-published.
- Use `npx @redocly/cli`, never `npx redocly`.
- In `apps/web`, TypeScript is pinned to 6.0.3 until `vue-tsc` supports TS 7.
- golangci-lint uses `//nolint:`; Semgrep uses `// nosemgrep: <rule-id>`. A justified exception needs both, with reasons. CI enforces errcheck, shadow, and US-spelling misspell.
- Go's `filepath.Glob` negates a class with `[^x]`, not `[!x]`.
- Update pixel baselines by dispatching the `Baselines` workflow (`.github/workflows/baselines.yml`, manual dispatch only) on the branch, downloading its `candidate-baselines` artifact, copying the files into place, and committing. It runs `make web-e2e-update` on a clean hosted checkout (the target itself still refuses a dirty tree) and never commits or pushes.
- `make docs-lint` runs Prettier over every Markdown file, ignored ones too.
- `deploy/web.Dockerfile` copies named paths only. When the web app starts importing a new directory, add it there too; CI builds from the full checkout and cannot catch the gap, only the release image build does.
- A push to `main` cancels the CI run of any earlier commit still in progress. Do not push while a release candidate's CI runs; release the newer commit instead if you must.
- Browser proofs withhold console output. A failure line ends with fixed diagnostic words (`lang-`, `header-`, `hydrated-`, `path-`, `el-`); read them before adding a debug branch.
- Avoid `podman ps | grep -q` under `pipefail`; capture output first.
- If host DB TCP fails while in-container `pg_isready` passes, recreate the container; rootless pasta can lose its forward.
- Production has no ad hoc database query path: the hosts run Bottlerocket and RDS is private.
