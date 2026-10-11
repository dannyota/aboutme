# Testing rules

Read this file when writing tests, authoring browser proofs, or selecting verification gates.

Tests inject clocks, randomness, and UUIDs and pin renderer inputs. Never retry a flaky test into a pass.

The table names required verification, not commands every worker must run locally. Use GitHub CI for these gates. The primary runs formatting and the static [pre-push check](resources.md#pre-push-check) before every push; any other local test, build, linter, or browser execution must follow [resources](resources.md). Reuse completed evidence when code has not changed.

|Change area|Command or evidence|
|-|-|
|Release candidate|Green GitHub CI on the pushed commit|
|Markdown people read|`node_modules/.bin/prettier --check` and `npx markdownlint-cli2` on touched files, outside the [agent-only paths](documentation.md#agent-file-formatting)|
|Resume schema or its types|`make schema-check`|
|OpenAPI|`make api-check`|
|Go server|`make server-build server-vet server-test` and `golangci-lint run ./...` over touched packages, tests included|
|Store or migrations|Go gate plus `make sqlc-check server-test-db server-test-integration server-migration-test`|
|Nuxt/Vue|`make web-lint web-typecheck web-test web-build`; after adding a web file, `make web-source-manifest-update`|
|Renderer or public page|Nuxt gate plus `make web-e2e`; baselines as in [web rules](web.md) and [Releases](releases.md#releases)|
|Authenticated UI|`make dev-https-auth-check dev-https-editor-check dev-https-mcp-check dev-https-entry-check`|
|Public surface|`make native-http-check` and `make dev-https-public-check`|
|Security-sensitive or release|CI's Semgrep and gitleaks jobs; diagnose their hosted logs without a local scan|

Reusable browser automation is scripted headless Playwright. To author it, use the Playwright MCP server in `.codex/config.toml` to inspect real selectors, requests, and state, then write what you observed as `@playwright/test` specs. MCP never runs the recorded automation. Report missing configuration or access to the primary.

- Browser proofs withhold console output. A failure line ends with fixed diagnostic words (`lang-`, `header-`, `hydrated-`, `path-`, `el-`); read them before adding a debug branch.
