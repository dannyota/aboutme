# Spec traceability matrix

**Completion target:** one row per normative statement in
[`../../design/`](../../design/README.md). Every phase plan must assign an
owner, state, and evidence before independent approval. Acceptance IDs are
stable and referenced by phase acceptance and UAT reports.

States are `PLANNED`, `LANDED`, `PROVEN`, or `BLOCKED`. A concrete test
reference is evidence, not a substitute for adjudicating the row. Each phase
closes its owned rows before its exit checklist.

## Ownership

The `Phase/task` column names the phase and tasks that own a row. Phase PM is
complete. Phase PV is active; see [PV](../phase-pv/README.md). A completed
phase's plan is deleted at exit, so its task IDs are history that Git keeps. The
test and acceptance references in each row remain the evidence.

## Matrix index

Rows live in one file per acceptance-ID prefix. Look a row up by its ID prefix;
rows are never split by number range.

| Prefix      | Rows | File                           |
| ----------- | ---- | ------------------------------ |
| `AC-DOC`    | 12   | [ac-doc.md](./ac-doc.md)       |
| `AC-AUTH`   | 18   | [ac-auth.md](./ac-auth.md)     |
| `AC-SAVE`   | 5    | [ac-save.md](./ac-save.md)     |
| `AC-MEDIA`  | 9    | [ac-media.md](./ac-media.md)   |
| `AC-PUB`    | 10   | [ac-pub.md](./ac-pub.md)       |
| `AC-SEC`    | 6    | [ac-sec.md](./ac-sec.md)       |
| `AC-RT`     | 2    | [ac-rt.md](./ac-rt.md)         |
| `AC-PDF`    | 1    | [ac-pdf.md](./ac-pdf.md)       |
| `AC-PRIV`   | 1    | [ac-priv.md](./ac-priv.md)     |
| `AC-OPS`    | 23   | [ac-ops.md](./ac-ops.md)       |
| `AC-INF`    | 8    | [ac-inf.md](./ac-inf.md)       |
| `AC-API`    | 2    | [ac-api.md](./ac-api.md)       |
| `AC-REN`    | 9    | [ac-ren.md](./ac-ren.md)       |
| `AC-FONT`   | 1    | [ac-font.md](./ac-font.md)     |
| `AC-EDITOR` | 17   | [ac-editor.md](./ac-editor.md) |
| `AC-MCP`    | 10   | [ac-mcp.md](./ac-mcp.md)       |
| `AC-UI`     | 13   | [ac-ui.md](./ac-ui.md)         |

Total: 147 rows across 17 prefixes.
