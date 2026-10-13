# Resume schema

## Sources and outputs

|Path|Role|
|-|-|
|`resume.schema.json`|Working current document schema|
|`released-versions.json`|Release manifest naming snapshots and retained output paths|
|`scripts/generate.mjs`|Generator entry point for current and retained Go and TypeScript outputs|
|`fixtures/`, `test/`|Accepted, rejected, bound, store, and code-generation examples and tests|
|`templates/`, `samples/`|Preset and sample sources for generated registries|
|`validation/`|Handwritten store validation, sanitizer policy, and hostile corpus|
|`gen/go/`, `gen/ts/`|Current and retained generated types, schema embeds, registries, and sanitizer output|

Before application `v1.0.0`, released snapshots, manifest entries, and older-client support may change without backwards compatibility. Application SemVer is separate from document schema versions. [ADR 0004](../../docs/adr/0004-resume-document-contract.md) owns the application v1 freeze and converter rules; [release rules](../../instructions/releases.md) own adding root `.v1-baseline`. See [package README](README.md) and [data design](../../docs/design/data.md).

`gen/go/section.go` and `gen/go/store_validate.go` are handwritten, despite their directory name; tests and `gen/go/cmd/conformance/` are also handwritten. Generated file headers identify the generator-owned files.

The generator also writes `apps/server/internal/showcase/presets.generated.go`. The web generator `apps/web/scripts/generate-document-validator.mjs` consumes the working schema to write `apps/web/app/editor/documentValidator.generated.mjs`. Briefs must explicitly name affected `gen/ts/` output and web validator output across the backend and frontend lanes.

## Authorities and checks

Read [Go rules](../../instructions/go.md), [web rules](../../instructions/web.md), [verification](../../instructions/verification.md), [data design](../../docs/design/data.md), [font design](../../docs/design/fonts.md), and [template contract](../../docs/design/templates/contract.md).

[Testing](../../instructions/testing.md) owns the schema gate. `test/` checks schema validity, the application v1 release freeze, generation drift, fixture conformance, template and sanitizer parity; `gen/go/` tests independently check embeds, section dispatch, and store validation. The released-schema guard is `scripts/check-released-schemas.sh`.
