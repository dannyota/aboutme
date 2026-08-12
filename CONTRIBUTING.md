# Contributing

Contributions should keep the design, implemented contracts, tests, and
documentation in agreement.

## Before changing code

1. Read the [design](docs/design/README.md), relevant
   [Architecture Decision Records](docs/adr/README.md), and the active
   [implementation plan](docs/plans/implementation-plan.md).
2. Check the [OpenAPI contract](docs/api/openapi.yaml), schema, code, and tests
   that own current behavior.
3. Add or update a failing test for behavior changes before implementing the
   smallest fix.

If the design, OpenAPI, code, and deployment configuration disagree, describe
the conflict in the issue or pull request. Do not choose one silently.

## Style and safety

- Follow the Google Go and TypeScript style guides. Use `gofmt`, `goimports`,
  and the configured ESLint rules.
- Keep comments focused on constraints the code cannot express. Put design
  rationale in `docs/design/` or an ADR.
- Use Mermaid for diagrams in Markdown.
- Never commit credentials or personal data. Add new configuration names with
  empty values to `.env.example`.
- Do not hand-edit generated artifacts or released migrations.

Documentation rules and lifecycle terms are in
[`docs/standards/engineering.md`](docs/standards/engineering.md).

## Checks

Run the narrowest check while developing, then the full local gate before
handoff:

```sh
make check
make ci
```

For documentation changes, run `make docs-fmt` and `make docs-lint`. The
[root Makefile](Makefile) lists component checks and generation drift gates.

## Pull requests

Describe the change and its purpose, name the affected contracts and tests, and
report the exact checks you ran. Link any design or ADR update needed to explain
a changed decision.

By contributing, you agree that your contribution is licensed under
[AGPL-3.0](LICENSE).
