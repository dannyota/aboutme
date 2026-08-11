# AC-REN traceability rows

6 acceptance-criterion rows with the `AC-REN-` prefix, in their original matrix
order. See [README.md](./README.md) for the matrix purpose, maintenance rules,
and the full prefix index.

| ID         | Spec clause | Statement                                                                                                                                                                                                                                  | Phase/task | Test / UAT reference |
| ---------- | ----------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ---------- | -------------------- |
| AC-REN-001 | §5          | Golden snapshots are deterministic and byte-stable across fixtures × template presets × both pagination modes, with the environment pinned (browser image and platform, self-hosted fonts, timezone, locale, no clock or random input)     | P3         | (pending)            |
| AC-REN-002 | §5          | Both pagination modes render correctly — editor-approximate breaks on entry boundaries, public is continuous — and a one-column layout renders main-then-sidebar with nothing silently unrendered                                          | P3         | (pending)            |
| AC-REN-003 | §5          | Fonts are self-hosted, load with no outbound network, cover the full Vietnamese diacritic set, and every snapshot or screenshot awaits font readiness before capture                                                                       | P3         | (pending)            |
| AC-REN-004 | ADR 0008    | Template presets validate against the customization schema, and applying one computes `layout.sections` as a total function of the document's own content keys (exactly-once by construction, per ADR 0008) while never touching `content` | P3         | (pending)            |
| AC-REN-005 | §5          | The editor→renderer one-way import boundary is lint-enforced: the renderer may not import from the editor                                                                                                                                  | P3         | (pending)            |
| AC-REN-006 | §5          | The renderer is pure: it renders under plain `vue/server-renderer` with no store, API, editor, locale or clock dependency, proven in a Node test environment rather than a DOM-providing one                                               | P3         | (pending)            |
