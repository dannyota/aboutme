# 0040: Contact labels beside icons, and scheme-free displayed addresses

Status: Accepted (2026-09-19)

Amends [ADR 0013](0013-contact-detail-rendering.md) (c) and its consequence that
icons "never suppress a detail's `label`".

## Context

With `header.iconStyle: outline`, a typed contact detail renders its icon and
its default label together, so a mail icon sits beside "Email:" and says the
same thing twice. Linked addresses render with their `https://` scheme and any
trailing slash, which adds noise on a resume and does not help a reader follow
the link.

## Decision

1. **Labels beside icons.** When the header shows icons, a typed detail omits
   its default label, because its icon already names the type. A non-empty user
   `label` still replaces the default and is shown, and a `custom` detail always
   shows its label, because its icon is generic. With `iconStyle: none`, labels
   render as ADR 0013 (c) states. A user label equal to the default counts as
   user-set and is shown.
2. **Displayed addresses.** An anchor shows its URL without the `https://`
   scheme and without one trailing slash. The query and fragment stay. The
   `href` keeps the full value. If nothing would remain, the full value is
   shown.

ADR 0013 (a) and (b) stand: array order, plain text for `email`, `phone`,
`location`, and `custom`, and the renderer's own exact `https://` re-check
before any anchor.

## Not decided

Rendering a `custom` detail whose value passes the same `https://` re-check as
an anchor was proposed with this record and is not decided. If accepted, it
becomes its own amendment to ADR 0013 (b).

## Consequences

- Icons now remove a default label; they still never add, remove, reorder, or
  reveal a detail, and never hide a user label or a value.
- Every golden and screenshot baseline with a linked address, or an icon beside
  a default-labelled detail, changed once with this decision.
- [Template contract §5.1](../design/templates/contract.md#51-header) and
  [token design §3.4](../design/templates/tokens.md#34-resume-header-treatment)
  state both rules.
