# Spec traceability matrix

**Completion target:** one row per normative statement in
[`../../design/`](../../design/README.md). Every phase plan must assign an
owner, state, and evidence before independent approval. Acceptance IDs are
stable and referenced by phase acceptance and UAT reports.

States are `PLANNED`, `LANDED`, `PROVEN`, or `BLOCKED`. A concrete test
reference is evidence, not a substitute for adjudicating the row. Each phase
closes its owned rows before its exit checklist.

## Active delivery graphs

| Phase | Plan                                            | Planned acceptance ownership                      |
| ----- | ----------------------------------------------- | ------------------------------------------------- |
| P4    | [Authenticated editor](../phase-4/README.md)    | `AC-EDITOR-001`–`AC-EDITOR-017`                   |
| P5A   | [Publish and public SSR](../phase-5a/README.md) | `AC-PUB`; P5A slices of `AC-OPS` and `AC-SEC-001` |

## Matrix index

Rows live in one file per acceptance-ID prefix. Look a row up by its ID prefix;
rows are never split by number range.

| Prefix      | Rows | File                           |
| ----------- | ---- | ------------------------------ |
| `AC-DOC`    | 12   | [ac-doc.md](./ac-doc.md)       |
| `AC-AUTH`   | 7    | [ac-auth.md](./ac-auth.md)     |
| `AC-SAVE`   | 5    | [ac-save.md](./ac-save.md)     |
| `AC-MEDIA`  | 9    | [ac-media.md](./ac-media.md)   |
| `AC-PUB`    | 5    | [ac-pub.md](./ac-pub.md)       |
| `AC-SEC`    | 4    | [ac-sec.md](./ac-sec.md)       |
| `AC-RT`     | 2    | [ac-rt.md](./ac-rt.md)         |
| `AC-PDF`    | 1    | [ac-pdf.md](./ac-pdf.md)       |
| `AC-PRIV`   | 1    | [ac-priv.md](./ac-priv.md)     |
| `AC-OPS`    | 20   | [ac-ops.md](./ac-ops.md)       |
| `AC-INF`    | 8    | [ac-inf.md](./ac-inf.md)       |
| `AC-API`    | 2    | [ac-api.md](./ac-api.md)       |
| `AC-REN`    | 9    | [ac-ren.md](./ac-ren.md)       |
| `AC-FONT`   | 1    | [ac-font.md](./ac-font.md)     |
| `AC-EDITOR` | 17   | [ac-editor.md](./ac-editor.md) |

Total: 103 rows across 15 prefixes.
