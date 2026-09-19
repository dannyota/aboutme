# 0043: Email and phone details render as links

Status: Accepted (2026-09-19)

Supersedes the clause of [ADR 0013](0013-contact-detail-rendering.md) (b) and
[ADR 0041](0041-contact-link-display-and-body-justify.md) Decision 1 that keeps
`email` and `phone` details plain text.

## Context

Readers of a resume expect to tap an email address or a phone number. ADR 0013
kept both as text because the schema defines no format for them and a link built
from an unchecked string could carry anything. That reason still holds for the
schema, so any link must come from a strict check in the renderer, the way the
`https://` re-check guards URL details.

## Decision

The schema is unchanged. The renderer links a detail only when its whole value
passes these checks, and otherwise renders the value as text.

1. **Email.** The href is `mailto:` followed by the value, when the value:
   - is 1 to 254 characters, counted as code points;
   - holds no Unicode white space, control, or format character, and none of
     ``< > " ' ` ( ) \ , ; : ? # %``;
   - has exactly one `@`, with at least one character before it;
   - has a part after `@` that contains a `.` and neither starts nor ends with
     one.
2. **Phone.** Remove every space (U+0020), `.`, `-`, `(`, and `)`. When the
   result matches `^\+?[0-9]{3,20}$` with ASCII digits, the href is `tel:`
   followed by that result. For example, `(+84) 374837720` becomes
   `tel:+84374837720`.
3. **Anchor text.** The anchor text is always the value as written. `display`
   does not apply to these links. Icons and the underline follow the other
   contact links. `location` and `custom` values that are not `https://` links
   stay text.

`?`, `#`, and `%` are excluded from addresses so that a value cannot add mail
headers such as `?bcc=` or a fragment to the `mailto:` URL.

## Consequences

- The Go package `internal/contactlink` and the web renderer implement the same
  rules. A shared corpus of cases, including hostile ones, keeps them identical.
- The public HTML validator accepts a `mailto:` or `tel:` anchor inside the
  resume header only when it equals an href derived by these rules from a
  visible detail, each once. Rich text keeps its own sanitized `mailto:` and
  `tel:` links.
- A value that fails a check still renders, as text, so no contact detail
  disappears.
