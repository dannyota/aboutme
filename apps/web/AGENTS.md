# Web application

## Source map

|Path|Role|
|-|-|
|`app/pages/`, `app/middleware/`|Routes and client route guards|
|`app/editor/`, `app/stores/`, `app/composables/`|Editor commands, optimistic state, and application state|
|`app/components/ui/`, `app/components/app/`|Generated UI primitives and shared application composites|
|`app/components/editor/`, `app/components/preview/`|Editor controls and preview controllers|
|`app/components/resume/`|[Pure renderer](app/components/resume/AGENTS.md)|
|`server/`|[Nitro routes and isolated render workers](server/AGENTS.md)|
|`app/i18n/`|Interface catalogs and locale behavior|
|`app/api/client.ts`, `app/api/generated/`|Handwritten client factory and generated contract types|
|`test/`, `e2e/`|Unit and Nuxt tests, browser proofs, pixel and print baselines|

Authenticated requests run client-side. Controllers supply authorized render input; renderer components do not read editor state or fetch data. Interface locale does not change resume language.

## Sources and outputs

`docs/api/openapi.yaml` feeds `scripts/openapi-gen.sh` and `app/api/generated/`; `scripts/api-drift-check.sh` verifies drift. An API brief must explicitly include this frontend output. See the [API guide](../../docs/api/AGENTS.md).

`packages/schema/scripts/generate.mjs` writes schema-generated TypeScript under `packages/schema/gen/ts/`. `scripts/generate-document-validator.mjs` derives `app/editor/documentValidator.generated.mjs` from `packages/schema/resume.schema.json`. A schema brief must name these outputs across lanes.

`scripts/ui-add.sh` generates UI primitives. Repository `scripts/generate-web-e2e-source-manifest.sh` writes `scripts/web-e2e-source.manifest`; the primary regenerates it when source coverage changes. Public-root generation is mapped in the [scripts guide](../../scripts/AGENTS.md).

## Authorities and checks

Read [web rules](../../instructions/web.md), [web design](../../docs/design/web.md), [localization](../../docs/design/localization.md), and [renderer architecture](../../docs/architecture/rendering-and-editor.md). [ADR 0019](../../docs/adr/0019-application-ui-toolkit.md) owns UI primitives.

[Testing](../../instructions/testing.md) names web lint, typecheck, unit, build, and browser gates. [Web rules](../../instructions/web.md) own source manifests and hosted Baselines; QA owns proofs and baseline evidence.
