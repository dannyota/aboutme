# Contributing

Thanks for your interest in aboutme!

## Ground rules

- **Design first.** Non-trivial changes start from the spec
  ([`docs/specs/aboutme-design.md`](docs/specs/aboutme-design.md)) and the ADRs
  in [`docs/adr/`](docs/adr/). If your change contradicts a recorded decision,
  open an issue to discuss before writing code.
- **Style.** Google style guides: [Go](https://google.github.io/styleguide/go/)
  and Google TypeScript style (enforced via ESLint). `gofmt`/`goimports` are
  mandatory.
- **Quality gates.** PRs must pass CI: linters (`golangci-lint`, ESLint,
  `vue-tsc`), tests, `govulncheck`, semgrep, schema-drift check, and
  `make docs-lint` for documentation.
- **Docs.** Mermaid (not ASCII) for diagrams in `.md` files. Run `make docs-fmt`
  before committing doc changes. Docs conventions:
  [`docs/README.md`](docs/README.md).
- **Secrets.** Never commit credentials. `.env` is git-ignored; add new
  variables to `.env.example` with empty values.

## Workflow

1. Open or comment on an issue describing the change.
2. Fork/branch, implement with tests.
3. `make docs-fmt` if docs changed; ensure CI is green.
4. Open a PR with a clear description of what and why.

## License

By contributing you agree that your contributions are licensed under
[AGPL-3.0](LICENSE).
