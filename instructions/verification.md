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

## GitHub CI

GitHub CI runs every build and test. `main` and other branches run it differently:

| | `main` | Other branches |
|-|-|-|
| Trigger | Every push runs `ci.yml` | Nothing runs on push; dispatch `gh workflow run ci.yml --ref <branch> -f base_sha=<sha>` |
| Release gate | A green push run on the exact commit is required to tag and deploy | Never counts as a release gate |
| Caches | Saves Go and tool caches | Restores caches but never saves them, so the first run can be slower |
| Canceling | A newer push to `main` cancels the older run | A newer dispatch on the same branch cancels the older run |

Before dispatching on a branch:

1. Merge `origin/main` into the branch (never rewrite pushed commits) so CI tests the branch as it will land.
2. Pass `base_sha=$(git merge-base origin/main HEAD)`: the full 40-character SHA of a commit that is an ancestor of the branch head. A SHA that is not an ancestor, such as a newer `main`, fails the migration and released-schema guards before they check anything.
3. Dispatch once and wait; do not dispatch again on the same branch while a run is in progress.
4. Read results for the exact head SHA (`gh run list --branch <branch>` shows `headSha`); a green run on an older commit proves nothing about the head.

After merging to `main`, the `main` push run is the gate: wait for it before tagging.


Find the cause before changing anything. Never rerun a failed job hoping for a pass, add retries or longer timeouts, or loosen a check without evidence that it is the cause.

1. Read the failing log and name the exact failing step. If the log hides the cause (for example the browser proofs withhold console output), add a diagnostic that records it (to a CI artifact, never secrets) on a debug branch that is never merged, and run that.
2. Bisect when a failure starts at a known point: find the first bad commit by CI runs on earlier commits.
3. Read the code on both sides and research the tool's docs and issues; cite sources in the report.
4. Fix the root cause, add a test that fails without the fix, and prove it with green CI on the exact head (twice for intermittent failures).
5. If only a local session can show the cause, stop and ask the manager with the exact commands and why CI cannot show it.
