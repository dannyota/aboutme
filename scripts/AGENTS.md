# Repository tooling

## Source map and ownership

The primary owns shared script entry points and manifests and assigns exact paths in briefs. Scripts under this directory orchestrate checks and generators; component scripts remain with their component owners.

|Family|Sources and tests|
|-|-|
|CI and scans|`ci.sh`, `ci-local.sh`, `ci-background.sh`, `ci-browser-proofs.sh`, `scan.sh`, `security-scan.sh`; corresponding `test/` suites|
|Static guards|`pre-push.sh`, `check-lengths.sh`, `check-tool-versions.sh`, `check-migrations-append-only.sh`, `check-released-schemas.sh`; regression suites under `test/`|
|Documentation|`check-doc-links.mjs` and `test/check-doc-links.test.mjs`|
|Local stacks and proofs|`dev-native.sh`, `dev-https.sh`, `lib/`, `dev-https-test.sh`, and browser staging helpers|
|Release and production evidence|`release-eligibility.sh`, `deployment-document-check.sh`, `totp-production-proof.sh`; matching `test/` scripts|
|Agent launch|`codex-agent.sh` and `test/codex-agent-test.sh`|
|Source archives|`web-e2e-source.sh`, `web-source-build.sh`, and source manifest generators; sibling tests|

## Generator sources and outputs

`generate-web-e2e-source-manifest.sh` writes `web-e2e-source.manifest`. `web-e2e-source.sh` stages that closed source set into immutable browser archives. The primary regenerates the shared manifest.

`generate-public-roots.mjs` consumes `packages/publicroots/public-roots.v11.json` and the build source manifests under `packages/publicroots/`. It writes `apps/server/internal/publicroots/generated.go`, `apps/web/app/public-roots.generated.ts`, `apps/web/test/public-roots.generated.test.ts`, `deploy/caddy/public-roots.generated.caddy`, and `deploy/caddy/testdata/public-roots.generated.json`. Briefs must explicitly include every affected cross-lane output.

Schema generation starts at `packages/schema/scripts/generate.mjs`; API client generation starts at `apps/web/scripts/openapi-gen.sh`; sqlc reads `apps/server/sqlc.yaml`. Their [schema](../packages/schema/AGENTS.md), [API](../docs/api/AGENTS.md), and [server](../apps/server/AGENTS.md) guides map sources and output ownership.

## Authorities and checks

Read [workflow](../instructions/workflow.md), [resource rules](../instructions/resources.md), [verification](../instructions/verification.md), [testing](../instructions/testing.md), and [documentation instructions](../instructions/documentation.md). Release scripts also follow [release rules](../instructions/releases.md).

Root `Makefile` wires script tests into operational suites. `test/pre-push-test.sh` checks formatter selection and execution guards; `test/codex-agent-test.sh` checks launcher wiring and settings validation using fixture roles. `packages/publicroots/public-roots.test.mjs` and `generate-web-e2e-source-manifest-test.sh` protect generated maps; hosted CI owns execution.

`check-path-case.sh` rejects case-folded index path collisions, including directory spelling clashes, in operational CI and static pre-push checks; `test/check-path-case-test.sh` covers index-only fixtures.
