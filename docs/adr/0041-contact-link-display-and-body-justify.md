# 0041: Custom https links, per-detail link display, and justified body text

Status: Accepted (2026-09-19)

Amends [ADR 0013](0013-contact-detail-rendering.md) (b) and (c), and
[ADR 0040](0040-contact-labels-beside-icons.md) Decision 2. Releases document
schema v3 under [ADR 0017](0017-resume-document-versioning.md).

## Context

A custom detail often holds a profile URL, such as Google Scholar or ORCID, but
ADR 0013 (b) renders every custom value as plain text, so the link cannot be
followed. ADR 0040 fixed how an address displays, while some owners want the
whole URL printed and others want a short clickable name such as "GitHub". Long
body paragraphs set ragged-right look uneven in dense templates, and some owners
want justified text. GitHub and Twitter render generic glyphs, so the header
does not show which service a link points to.

## Decision

1. **Custom links.** A `custom` detail whose value passes the renderer's own
   `https://` re-check renders as an anchor with `rel="noopener noreferrer"` and
   the generic link icon. The check is the one the four URL types already use:
   an exact lowercase `https://` prefix, checked at render time and never
   trusted from write. Any other custom value stays plain text. The schema keeps
   custom values free text; email, phone, and location stay plain text.
2. **Link display.** A detail gains optional `display`: `short`, `full`, or
   `label`. Absent means `short`, the ADR 0040 address without scheme or
   trailing slash. `full` shows the whole URL. `label` shows the detail's label,
   else the type's default label, as the anchor text, with no separate label
   prefix. `display` has no effect on a value that renders as text.
3. **Justified body text.** Customization gains optional `font.textAlign`:
   `left` or `justify`, absent meaning `left`. Justify applies to entry and
   summary body paragraphs and list items, with `hyphens: auto` driven by the
   resume's language. Headings, the header, dates and meta lines, contact rows,
   and skill or language tags never justify. Screen, paged preview, public page,
   and PDF share the rule.
4. **Brand marks.** GitHub and Twitter render the GitHub and X marks from Simple
   Icons (CC0-1.0), inline and monochrome in the icon colour. LinkedIn keeps the
   generic link glyph, because Simple Icons no longer ships it and no CC0 source
   exists. The `twitter` type's default label becomes "X"; the type name stays
   `twitter`.

## Document version

`display` and `textAlign` do not fit document v2, whose objects reject unknown
properties, so they release as document v3. The server accepts and emits v1, v2,
and v3. Emitting v2 drops `display` and `textAlign`; that is a declared loss
beside the v1 font fallback. A v1 or v2 client write keeps the stored
`textAlign` and the stored `display` of every detail that survives the write,
matched by id, because an older client cannot express either field. Detail ids
must therefore be unique; store validation rejects a repeated id.

## Consequences

- Hostile custom values (`javascript:`, `data:`, protocol-relative, uppercase
  scheme, leading whitespace) stay text in every display mode; the renderer
  tests pin them.
- A template switch carries `font.textAlign` over, so presets do not own it.
- The public Markdown export ignores `display`: it prints each contact's label
  and value, and links a custom value that passes the same `https://` check.
  JSON-LD `sameAs` lists every URL-type or custom value that passes the check.
- A release that stores v3 cannot be rolled back to an earlier release: the
  older release fails closed on any resume saved as v3. Fix forward; the
  [production runbook](../runbooks/production.md#rollback) records this.
- [Template contract §5.1](../design/templates/contract.md#51-header),
  [token design §2 and §3.4](../design/templates/tokens.md), and the
  [data design](../design/data.md#document-versions) state these rules.
- Brand-mark sources and licences are recorded in `THIRD_PARTY_NOTICES.md`.
