# 0042: Owner-set public page title and emoji favicon

Status: Accepted (2026-09-19)

## Context

A public resume page's browser tab always reads `<full name> — Resume` and uses
the site icon. Owners want to set a custom tab title and emoji favicon for each
resume. The public page head is closed: the server's HTML validator accepts only
the exact values it expects, so any per-resume head value must be computed and
checked by the server.

## Decision

1. **Publication settings, not document fields.** `publicTitle` and
   `faviconEmoji` live on the resumes row beside the slug and the download and
   discovery flags, added by migration `00003`. The resume document and its
   schema version are unchanged. The owner sets them through the existing
   publish request: absent keeps the stored value, an empty string clears it,
   and `null` is malformed. The owner resource returns both, `null` when unset.
   The public JSON does not carry them; only the HTML head uses them.
2. **Validation.** The server trims Unicode white space, then:
   - `publicTitle` is at most 70 grapheme clusters and 560 code points
     (`too_long`), with no Unicode control characters and no format characters
     other than U+200D ZERO WIDTH JOINER (`invalid_characters`). That rules out
     bidi overrides and isolates and zero-width characters. The title is text
     and the page escapes it.
   - `faviconEmoji` is exactly one grapheme cluster of at most 16 code points:
     an Extended_Pictographic base followed only by other Extended_Pictographic
     characters, ZWJ, variation selectors, skin-tone modifiers, or tags; or one
     flag of two Regional Indicators (`invalid_emoji`). Grapheme clusters come
     from `github.com/rivo/uniseg` v0.4.7. Extended_Pictographic comes from a
     table generated from Unicode 17.0.0 `emoji-data.txt` by a committed
     generator.
3. **Rendering.** The server computes the exact `<title>`: the owner's title or
   `<full name> — Resume`. When an emoji is set, it also computes the exact
   favicon URL: `data:image/svg+xml,` followed by an SVG holding the emoji in a
   `<text>` element, with every byte outside the URL-unreserved set
   percent-encoded. It passes both to the renderer. The page emits
   `<link rel="icon" href="…">` only when an emoji is set.
4. **Validation of output.** The public HTML validator accepts only that exact
   title and, when set, exactly one `<link rel="icon">` with exactly `rel` and
   the exact `href`. Any other icon link, a second one, or an icon when none is
   set is rejected. The page CSP already allows `data:` images through
   `img-src 'self' data:`, which browsers apply to icons.

## Consequences

- Both settings are public, like the slug: they appear in the page head of
  anyone who opens the page. They are resume content the owner writes, so they
  add no new category of personal data; the account export includes them.
- The editor may pre-validate with the same rules (`app/utils/publicPage.ts`). A
  shared corpus of hostile inputs keeps the Go and TypeScript checks in
  agreement; the server stays authoritative.
- Emoji added after Unicode 17.0 are rejected until the table is regenerated.
- Saving either setting bumps the revision like any publish, so cached pages and
  live readers refresh.
