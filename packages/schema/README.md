# Resume schema package

`packages/schema` owns the resume document contract, immutable releases,
generated Go and TypeScript types, shared validation fixtures, and template
preset data. The [data design](../../docs/design/data.md) defines the intended
model.

## Sources and generated output

| Path                     | Role                                                                  |
| ------------------------ | --------------------------------------------------------------------- |
| `resume.schema.json`     | Working JSON Schema 2020-12 source for the current document version   |
| `resume.v*.schema.json`  | Immutable released snapshots                                          |
| `released-versions.json` | Append-only manifest of released snapshots and retained types         |
| `scripts/generate.mjs`   | Sole generator entry point                                            |
| `gen/go/` and `gen/ts/`  | Committed current and retained generated types and version registries |

Do not hand-edit generated files. Release a new document version by adding its
snapshot and manifest entry, updating the working schema, then regenerating.
Never change a released snapshot or manifest entry.

Document schema v4 is current. The registry accepts and emits document versions
1 through 4. Document schema v2 uses the licensed font catalog's stable IDs
while retaining v1's immutable display-name identifiers. See the
[font catalog design](../../docs/design/fonts.md).

## Validation data

- `fixtures/` holds accepted and rejected schema examples.
- `fixtures/bounds/` and its manifest cover every numeric document bound.
- `fixtures/store/` covers aggregate rules and hostile inputs that may be
  invalid at the schema layer.
- `validation/store.ts` and `gen/go/store_validate.go` implement matching
  cross-field checks that JSON Schema cannot express.
- `validation/sanitizer-allowlist.v1.json` and `validation/hostile-corpus.json`
  define the generated cross-runtime sanitizer policy and conformance inputs.

The Go and TypeScript validators consume the same fixture corpus. Divergent
behavior fails both suites.

## Template presets

`templates/` contains 20 committed preset JSON files. The web app uses the
presets, renderer, and licensed font catalog. See the
[template design](../../docs/design/templates/README.md).

## Commands

Run from this directory:

```sh
npm ci
npm run generate
npm test
```

From the repository root, `make schema-gen` regenerates and `make schema-check`
runs the package gate.
