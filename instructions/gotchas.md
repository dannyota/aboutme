# Common tool gotchas

Read this file when running repository tools. Task-specific gotchas live in [Go](go.md), [web](web.md), [deployment](deploy.md), and [testing](testing.md). CI concurrency and exact-commit release evidence live in [verification](verification.md#github-ci).

- `.env` is ignored and holds credentials. Never print, document, or stage values; add names with empty values to `.env.example`.
- Run `make` at the repository root.
- Use `npx @redocly/cli`, never `npx redocly`.
- `make docs-lint` runs Prettier over every Markdown file, ignored ones too.
