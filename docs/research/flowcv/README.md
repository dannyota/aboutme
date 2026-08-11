# FlowCV research

Competitor research on FlowCV's CV product surface, compiled 2026-08-11 from the
local reference notes at `~/src/flowcv/docs/` (`API.md`, `RENDERING.md`,
`PULLPUSH.md`).

**This directory is evidence, not project authority.** Per `AGENTS.md`, external
references inform decisions but never override the design spec, ADRs, or the
frozen schema. Nothing here has been verified against the live FlowCV API, and
rows marked INFERRED are readings, not facts. Adopting anything below requires a
spec or ADR decision first.

Scope is the resume document only: structure, sections, entries, rich text,
layout, templates, typography, colour, photo, pagination, print, sharing and
export. FlowCV's other products are named once and not analysed.

## Documents

- [Feature inventory](feature-inventory.md) — 74 CV capabilities with per-row
  evidence and an OBSERVED/INFERRED mark.
- [Gap analysis](gap-analysis.md) — each capability against the aboutme design
  spec, with 3 `v1`, 9 `post-v1` and 62 `skip` recommendations.
