# 0044: Header photo position and project subtitle

Status: Accepted (2026-09-19)

Releases document schema v4 under
[ADR 0017](0017-resume-document-versioning.md).

## Context

Every template places the header photo above the name. Many resumes put the
photo beside the name and contact details instead, which saves vertical space on
the first page. Project entries have no subtitle slot, so owners put a project's
stack or role in an italic body paragraph, which templates cannot style as entry
metadata.

## Decision

1. **Photo position.** `customization.header` gains optional `photoPosition`:
   `top`, `left`, or `right`. Absent means `top`, the layout every document had
   before. With `left` or `right`, the photo and the text block (name, headline,
   and contact details) sit side by side:
   - The photo is on the chosen side, vertically centered against the text
     block, at `--photo-size` with a 1.25em gap, and never shrinks.
   - The text block takes the remaining width. `header.align` applies inside it
     only: `center` centers the name, headline, and details in the text column,
     and the photo stays at its edge.
   - `detailsLayout` and `iconStyle` apply inside the text block unchanged.
   - With no photo, the setting has no effect.
   - On a continuous page shown on a screen narrower than 36em (576 px), a side
     photo stacks above the text, as with `top`. The rule is a screen media
     query, not a container query: a container query needs size containment,
     which changes how the shrink-to-fit print and harness layouts measure
     width. Printed and paged output, the PDF and the paged preview, always
     keeps the side layout.
   - A template switch keeps the owner's `photoPosition`, or its absence, as it
     keeps `font.textAlign`. A preset with no header gets the default header
     (`left`, `inline`, `outline`) plus the kept position, which renders the
     same as no header.
2. **Project subtitle.** A project entry gains optional `subtitle`: plain text,
   at most 160 characters, the same bound as a custom entry's subtitle. It
   renders under the title in the entry's subtitle slot, with the same style as
   custom and certificate subtitles, as plain text and never a link.

## Document version

Both fields are new properties on objects that reject unknown properties, so
they release as document v4. The server accepts and emits v1 to v4. Emitting v3
or older drops `photoPosition` and every project `subtitle`; that is a declared
loss beside the v3 and v1 losses. A v1 to v3 client write keeps both stored
values, because an older client cannot express either field:

- The stored `photoPosition` returns while both the stored and the written
  document have a header. A client that removes the header removes the position
  with it.
- Each project entry that survives the write keeps its stored `subtitle`,
  matched by entry id. An entry the client added gets none.

## Consequences

- The header markup gains `data-photo-position="left"` or `"right"` on
  `header.resume-header` when a photo renders beside the text. The name,
  headline, and details sit in `div.resume-header-text` in every header.
- The public HTML validator needs no change: it checks the photo `img` and that
  each contact link sits inside `header.resume-header`, and both still hold.
- Public JSON carries both fields. The public Markdown export prints a project
  subtitle on its own line under the title, as for custom entries, and ignores
  `photoPosition`.
- A release that stores v4 cannot be rolled back to an earlier release: the
  older release fails closed on any resume saved as v4. Fix forward; the
  [production runbook](../runbooks/production.md#rollback) records this.
- [Template contract §5.1 and §5.2](../design/templates/contract.md#51-header),
  [token design §3.4](../design/templates/tokens.md), and the
  [data design](../design/data.md#document-versions) state these rules.
