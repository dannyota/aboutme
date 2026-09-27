# 0013: Resume header, contact details, and body text rendering

Status: Accepted (2026-08-11, 2026-09-19).

## Context

`ResumeHeader`'s contact details are golden-visible, so their order, labels, and
link rules must be settled before snapshots are committed. The schema restricts
the four URL types (`website`, `linkedin`, `github`, `twitter`) to an exact
lowercase `https://` value. It gives `email`, `phone`, `location`, and `custom`
no format, and says not to extend them without a design or ADR decision. Any
link built from those values must therefore come from a strict check in the
renderer.

Owners also asked for icons without duplicated labels, readable addresses,
followable profile links in custom details, service brand marks, a photo beside
the name, a project subtitle, and justified body text.

## Decision

### Contact details

1. **Order.** Display order is the array order of `personalDetails.details`.
   There is no separate order field. A detail with `isHidden: true` is omitted
   before ordering is observable.
2. **Labels.** A non-empty `personalDetail.label` replaces the type's default
   label; absent or `""` uses the default. When the header shows icons
   (`header.iconStyle` other than `none`), a typed detail omits its default
   label because its icon names the type. A user label is still shown, even when
   it equals the default, and a `custom` detail always shows its label because
   its icon is generic. With `iconStyle: none`, every detail shows its label.
3. **URL links.** The four URL types, and a `custom` value, render as an anchor
   with `rel="noopener noreferrer"` only when the value passes the renderer's
   own check for an exact lowercase `https://` prefix, made at render time and
   never trusted from write. Any other value renders as text. The schema keeps
   custom values free text. A linked custom detail uses the generic link icon.
4. **Link display.** A detail has optional `display`: `short`, `full`, or
   `label`. Absent means `short`: the URL without the `https://` scheme and
   without one trailing slash, keeping query and fragment; if nothing would
   remain, the full value shows. `full` shows the whole URL. `label` shows the
   detail's label, else the type's default label, as the anchor text, with no
   separate label prefix. The `href` always keeps the full value. `display` has
   no effect on a value that renders as text.
5. **Email links.** The href is `mailto:` followed by the value when the value:
   - is 1 to 254 characters, counted as code points;
   - holds no Unicode white space, control, or format character, and none of
     ``< > " ' ` ( ) \ , ; : ? # %``;
   - has exactly one `@`, with at least one character before it;
   - has a part after `@` that contains a `.` and neither starts nor ends with
     one.

   `?`, `#`, and `%` are excluded so a value cannot add mail headers such as
   `?bcc=` or a fragment.

6. **Phone links.** Remove every space (U+0020), `.`, `-`, `(`, and `)`. When
   the result matches `^\+?[0-9]{3,20}$` with ASCII digits, the href is `tel:`
   followed by that result; for example `(+84) 374837720` becomes
   `tel:+84374837720`.
7. **Email and phone anchor text** is always the value as written; `display`
   does not apply. `location` values, and custom values that are not `https://`
   links, stay text. A value that fails a check still renders as text, so no
   detail disappears.
8. **Brand marks.** GitHub and Twitter render the GitHub and X marks from Simple
   Icons (CC0-1.0), inline and monochrome in the icon color. The `twitter`
   type's default label is "X"; the type name stays `twitter`. LinkedIn renders
   Font Awesome Free's square LinkedIn mark (CC BY 4.0) the same way. Icons are
   presentation: they never add, remove, reorder, or reveal a detail, and never
   hide a user label or a value.

### Header photo and project subtitle

- **Photo position.** `customization.header.photoPosition` is `top`, `left`, or
  `right`; absent means `top`. With `left` or `right`, the photo and the text
  block (name, headline, contact details) sit side by side:
  - The photo is on the chosen side, vertically centered against the text block,
    at `--photo-size` with a 1.25em gap, and never shrinks.
  - The text block takes the remaining width. `header.align` applies inside it
    only; `detailsLayout` and `iconStyle` apply inside it unchanged.
  - With no photo, the setting has no effect.
  - On a continuous page on a screen narrower than 36em (576 px), a side photo
    stacks above the text. The rule is a screen media query, not a container
    query, because a container query needs size containment, which changes how
    the shrink-to-fit print and harness layouts measure width. Printed and paged
    output always keeps the side layout.
- **Project subtitle.** A project entry has optional `subtitle`: plain text, at
  most 160 characters. It renders under the title in the entry's subtitle slot,
  styled like custom and certificate subtitles, and never as a link.

### Body text

- **Justified text.** `customization.font.textAlign` is `left` or `justify`;
  absent means `left`. Justify applies to entry and summary body paragraphs and
  list items, with `hyphens: auto` driven by the resume's language. Headings,
  the header, dates and meta lines, contact rows, and skill or language tags
  never justify. Screen, paged preview, public page, and PDF share the rule.
- **Template switches** keep the owner's `font.textAlign` and
  `header.photoPosition`, or their absence; presets do not own them. A preset
  with no header gets the default header (`left`, `inline`, `outline`) plus the
  kept position.

## Consequences

- The renderer's own `https://` re-check is defense in depth against a document
  stored before the pattern tightened or through a path that skipped validation.
  It must not be removed as redundant. Hostile custom values (`javascript:`,
  `data:`, protocol-relative, uppercase scheme, leading whitespace) stay text in
  every display mode; renderer tests pin them.
- `internal/contactlink` in Go and the web renderer implement the email and
  phone rules, and a shared corpus of cases, including hostile ones, keeps them
  identical.
- The public HTML validator accepts a `mailto:` or `tel:` anchor inside the
  resume header only when it equals an href derived by these rules from a
  visible detail, each once. Rich text keeps its own sanitized `mailto:` and
  `tel:` links.
- The header markup carries `data-photo-position="left"` or `"right"` on
  `header.resume-header` when a photo renders beside the text; name, headline,
  and details sit in `div.resume-header-text` in every header.
- The public Markdown export ignores `display` and `photoPosition`: it prints
  each contact's label and value, links a custom value that passes the check,
  and prints a project subtitle on its own line under the title. JSON-LD
  `sameAs` lists every URL-type or custom value that passes the check.
- `display`, `textAlign`, `photoPosition`, and project `subtitle` arrived with
  document v3 and v4; ADR 0004 holds their version and loss rules.
- Brand-mark sources and licenses are recorded in `THIRD_PARTY_NOTICES.md`.
- [Template contract §5.1 and §5.2](../design/templates/contract.md#51-header),
  [token design §2 and §3.4](../design/templates/tokens.md), and the
  [data design](../design/data.md#document-versions) state these rules.

## History

- Former ADR 0013 (2026-08-11): array order, plain-text contacts except the four
  URL types, and `label`.
- Former ADR 0040 (2026-09-19): icons replace default labels; scheme-free
  displayed addresses.
- Former ADR 0041 (2026-09-19): custom `https://` links, `display`, justified
  body text, and brand marks, released as document v3.
- Former ADR 0043 (2026-09-19): `mailto:` and `tel:` links after strict checks,
  replacing the plain-text rule for email and phone.
- Former ADR 0044 (2026-09-19): header photo position and project subtitle,
  released as document v4.
