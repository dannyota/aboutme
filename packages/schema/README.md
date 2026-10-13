# Resume schema package

`packages/schema` owns the resume document contract, released snapshots,
generated Go and TypeScript types, shared validation fixtures, and template
preset data. The [data design](../../docs/design/data.md) defines the intended
model.

## Sources and generated output

| Path                     | Role                                                                  |
| ------------------------ | --------------------------------------------------------------------- |
| `resume.schema.json`     | Working JSON Schema 2020-12 source for the current document version   |
| `resume.v*.schema.json`  | Released document snapshots                                           |
| `released-versions.json` | Manifest of released snapshots and retained types                     |
| `scripts/generate.mjs`   | Sole generator entry point                                            |
| `gen/go/` and `gen/ts/`  | Committed current and retained generated types and version registries |

Do not hand-edit generated files. Before application `v1.0.0`, snapshots and
manifest entries may change without backwards compatibility. Application SemVer
is separate from document schema version numbers. Existing converters may
remain; fallback machinery is not required solely for older v0 clients.

After the application v1 baseline, add new document versions through a snapshot
and manifest entry, update the working schema, then regenerate. Existing
released snapshots and manifest entries remain unchanged.
[ADR 0004](../../docs/adr/0004-resume-document-contract.md) owns this boundary,
and [release rules](../../instructions/releases.md) define the first v1
candidate.

The [release manifest](released-versions.json) names the current, accepted, and
emitted document versions. See
[resume data](../../docs/architecture/resume-data.md) for runtime projection and
mutation behavior, and the [font catalog design](../../docs/design/fonts.md) for
font identifier compatibility.

## Validation data

- `fixtures/` holds accepted and rejected schema examples.
- `fixtures/bounds/` and its manifest cover every numeric document bound.
- `fixtures/store/` covers aggregate rules and hostile inputs that may be
  invalid at the schema layer.
- `validation/store.ts` and `gen/go/store_validate.go` implement matching
  cross-field checks that JSON Schema cannot express.
- `validation/sanitizer-allowlist.v1.json` and `validation/hostile-corpus.json`
  define the generated cross-runtime sanitizer policy and conformance inputs.

## Template presets

`templates/` contains the committed preset JSON sources. See the
[template design](../../docs/design/templates/README.md) and
[renderer architecture](../../docs/architecture/rendering-and-editor.md#pure-renderer).

## Commands

Run from this directory:

```sh
npm ci
npm run generate
npm test
```

From the repository root, `make schema-gen` regenerates and `make schema-check`
runs the package gate.
