# Engineering rules and verification

Code rules and the checks each change area needs. Implementers, qa, reviewers, and leads read this file.

- Pin the latest stable dependency at scaffold time; upgrades need review. Exact tool versions live in `.tool-versions`; `make tools-check` rejects drift. When an installed tool is newer than the pin, update `.tool-versions` and every mirror, then run the affected checks.
- Follow Google Go and TypeScript style with `gofmt`/`goimports` and the configured ESLint. Tests inject clocks, randomness, and UUIDs and pin renderer inputs. Never retry a flaky test into a pass.
- Never hand-edit generated files; change the source and regenerate. Migrations are append-only; roll back with a forward migration and grant `aboutme_app` explicitly ([ADR 0038](../docs/adr/0038-single-baseline-and-plain-migrator.md)).
- A contract change updates schema or OpenAPI sources, generated clients, tests, examples, design docs, and traceability in one change.
- Do not weaken security controls: least privilege, strict input bounds, versioned sanitizing, CSRF and Origin checks, `__Host-` cookies, route-specific rate limits, CSP, secret-free logs.

The table names required verification, not commands every worker must run locally. Use GitHub CI for these gates. Local formatting and static inspection may stay small; any test, build, linter, or browser execution must follow [resources](resources.md). Reuse completed evidence when code has not changed.

| Change area | Command or evidence |
|-|-|
| Release candidate | Green GitHub CI on the pushed commit |
| Markdown people read | `node_modules/.bin/prettier --check` and `npx markdownlint-cli2` on touched files, outside the agent-only paths in AGENTS.md |
| Resume schema or its types | `make schema-check` |
| OpenAPI | `make api-check` |
| Go server | `make server-build server-vet server-test` and `golangci-lint run ./...` over touched packages, tests included |
| Store or migrations | Go gate plus `make sqlc-check server-test-db server-test-integration server-migration-test` |
| Nuxt/Vue | `make web-lint web-typecheck web-test web-build`; after adding a web file, `make web-source-manifest-update` |
| Renderer or public page | Nuxt gate plus `make web-e2e`; baselines as in [Releases](releases.md#releases) |
| Authenticated UI | `make dev-https-auth-check dev-https-editor-check dev-https-mcp-check dev-https-entry-check` |
| Public surface | `make native-http-check` and `make dev-https-public-check` |
| Security-sensitive or release | CI's Semgrep and gitleaks jobs; diagnose their hosted logs without a local scan |

Reusable browser automation is scripted headless Playwright. To author it, use the Playwright MCP server to inspect real selectors, requests, and state, then write what you observed as `@playwright/test` specs. MCP never runs the recorded automation.
