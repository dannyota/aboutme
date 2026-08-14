# Phase 5A public render and byte formats

Status: **Approved — ready for implementation planning.**

This document owns the private render envelope and budgets, exact markdown and
discovery bytes, and the closed JSON-LD form. The
[public contract](public-contract.md) owns routes, responses, projections, and
validators. The [core design](design.md) owns domain policy.

## Internal public-render contract

Go calls exact `POST /internal-render/public` on the configured direct Nuxt
origin. The request is `application/json; charset=utf-8`, has no cookies,
authorization, viewer forwarding headers, or ambient credentials, and is a
closed object:

```text
{
  publicResume: PublicResume,
  mode: "continuous",
  canonicalOrigin: string,
  discoveryEnabled: boolean
}
```

Nuxt rejects unknown fields, wrong mode, unsupported schema version, malformed
origin, or photo-context mismatch. It performs no ID, API, database, session,
public-state, or network lookup. It adapts a public photo to the current
renderer schema with exact unused key marker `public-render-photo` and passes
only its public URL through `RenderContext`. Hydration contains only
`publicResume`; no private key enters Nuxt.

The response is one complete `text/html; charset=utf-8` body. Go validates
status, type, length, and the accepted CSP/inline-script contract before sending
a viewer success. It cancels the direct request with its viewer/generation
context. Nuxt work must stop before the handler returns.

## Internal-render budgets

The landed [numeric budget authority](../budgets.md#provenance-of-the-p5a-rows)
owns these values:

| Bound                   | Value           | Provenance                                        |
| ----------------------- | --------------- | ------------------------------------------------- |
| Canonical document      | 524,288 B       | Current document authority                        |
| `canonicalOrigin`       | 512 ASCII bytes | Parsed origin maximum; no userinfo/path/query     |
| Internal JSON request   | 532,480 B       | Document maximum plus 8,192-byte envelope         |
| Internal HTML response  | 2,097,152 B     | Four times document ceiling; measured gate        |
| Direct render wall time | 5 seconds       | 12.5× current 400 ms p95 SLO; cancelable hard cap |

`canonicalOrigin` must be an exact normalized `http` or `https` origin with no
userinfo, non-root path, query, or fragment. Production requires `https`; local
development may use configured `http`. Its ASCII serialization is at most 512
bytes.

The request-size proof has one document-shaped value, not two. Projection only
deletes values except that one photo key of at least one and at most 512 bytes
may become an absolute URL of at most 571 bytes. Slug, revision, language,
booleans, four fixed field names, braces, quotes, and separators fit within
2,048 bytes. The remaining margin exceeds the worst 570-byte photo growth. A
boundary test must construct the largest valid canonical document and exact
512-byte origin, serialize the closed envelope, and prove the 532,480-byte
ceiling. The direct route does not inherit the public API's 256 KiB body limit.

The 2 MiB HTML value is provisional until the minimal, full, and 512 KiB
renderer corpus passes beneath it without truncation. A breach blocks the phase
or changes the budget with evidence. The five-second timer cancels and joins
Nuxt work; it never returns while detached rendering continues.

## Markdown bytes

Markdown is UTF-8 with LF, no trailing spaces, no more than one blank line, and
one final LF. Build an ordered list of nonempty blocks, join blocks with exactly
`\n\n`, then append `\n`.

Plain values backslash-escape every CommonMark punctuation byte: `` ` ``, `\\`,
`*`, `_`, `{`, `}`, `[`, `]`, `<`, `>`, `(`, `)`, `#`, `+`, `-`, `.`, `!`, and
`|`. Heading and link text use the same escape. Link destinations use the stored
validated absolute URI with ASCII space, `(`, `)`, and control bytes
percent-encoded using uppercase hex.

Before escaping a plain line, replace tab, CR, LF, U+2028, and U+2029 with
U+0020, collapse U+0020 runs, and trim U+0020 at both ends. This prevents a
stored line break or indentation from creating Markdown structure.

Blocks are:

1. `# <fullName>`.
2. Nonblank headline as one escaped line.
3. All visible contacts as one block of LF-joined list items.
4. Sections in `layout.sections.main`, then `.sidebar`, with entries in stored
   order.

Contact syntax is `- <label>: <value>`. A non-empty custom label wins; otherwise
labels are exactly `Email`, `Phone`, `Location`, `Website`, `LinkedIn`,
`GitHub`, `Twitter`, and `Detail`. Website, LinkedIn, GitHub, and Twitter values
are `[<value>](<destination>)`; all other types remain plain text.

A nonblank section `displayName` starts its own `## <displayName>` block. An
absent or blank display name adds no substitute heading. Each entry is one
block. Its lines are fixed by this table; absent or blank lines are omitted:

| Type          | Heading line                | Secondary line | Meta line                 | Body        |
| ------------- | --------------------------- | -------------- | ------------------------- | ----------- |
| `profile`     | none                        | none           | none                      | `text`      |
| `work`        | `### jobTitle`              | employer       | dates, then city/country  | description |
| `education`   | `### degree`                | school         | dates, then city/country  | description |
| `skill`       | `### name`                  | none           | `Level: n/5` when present | `infoHtml`  |
| `language`    | `### name`                  | none           | `Level: n/5` when present | none        |
| `certificate` | linked or plain `### title` | issuer         | single date               | description |
| `project`     | linked or plain `### title` | none           | dates                     | description |
| `custom`      | linked or plain `### title` | subtitle       | dates, then city          | description |

`work.employer` uses `employerLink`; `education.school` uses `schoolLink`;
`certificate.title` and `custom.title` use `titleLink`; and `project.title` uses
`link`. A linked heading is exactly `### [<title>](<destination>)`. Secondary
linked text is exactly `[<value>](<destination>)`. Present entry lines emit in
table order: heading, secondary, meta, then body, with exactly one LF between
lines and no extra blank line inside the entry except those produced inside its
rich-text body.

City and country join as `<city>, <country>` when both exist; one present value
stands alone. Date rendering uses document `dateFormat`: year-only is `YYYY`;
month forms are zero-padded `MM/YYYY` or fixed English `Jan`…`Dec`, U+0020, then
`YYYY`; ranges are `<start> – <end>` using U+2013 with one space each side;
present end is `Present`. Certificate uses one date. Multiple present meta
values join with exact U+0020, U+00B7 MIDDLE DOT, U+0020.

Sanitized rich text converts deterministically after HTML entity decoding. In
inline text, CR/LF/tab/form-feed/U+0020 runs become one U+0020 and block edges
trim it. `p` becomes a block; `br` becomes one reverse solidus followed by LF,
the CommonMark hard-break form. `strong` becomes `**text**`, `em` becomes
`*text*`, and `u` keeps plain content. `a` becomes `[text](destination)`.
Unordered items use `-` followed by U+0020. Each ordered list is renumbered from
decimal `1`, `.`, and U+0020. Nested lists indent two spaces per level. Adjacent
paragraphs and lists have one blank line. Empty nodes emit nothing. No HTML is
emitted.

## Aggregate discovery bytes

`/sitemap.xml` uses XML 1.0 UTF-8. Eligible canonical HTML URLs are bytewise
slug-ordered and XML-escaped. Exact output is:

```xml
<?xml version="1.0" encoding="UTF-8"?>
<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
  <url><loc>https://aboutme.vn/first-slug</loc></url>
  <url><loc>https://aboutme.vn/second-slug</loc></url>
</urlset>
```

The two URL lines repeat zero or more times; configured canonical origin
replaces the example origin. There is one final LF and no `lastmod`, priority,
title, name, or document field.

`/llms.txt` is exact fixed heading followed by bytewise slug-ordered URLs:

```text
# aboutme resumes
- https://aboutme.vn/first-slug
- https://aboutme.vn/second-slug
```

There is one final LF. With no eligible resume it is exactly
`# aboutme resumes\n`. It contains no description or resume field.

`/robots.txt` is static except configured origin and ends with one LF:

```text
User-agent: *
Allow: /
Sitemap: https://aboutme.vn/sitemap.xml
```

Its private-cache format version is decimal `1` for this contract revision; any
later byte or format change increments that value.

## JSON-LD and CSP

Discoverable HTML contains exactly one JSON-LD script. Nondiscoverable HTML
contains none. The closed schema is:

```text
{
  "@context": "https://schema.org",
  "@type": "ProfilePage",
  "url": canonical current resume URL,
  "name": "<fullName> — Resume",
  "inLanguage": canonical lng,
  "mainEntity": {
    "@type": "Person",
    "name": fullName,
    "description"?: nonblank headline,
    "image"?: authorized public photo URL,
    "sameAs"?: unique visible website/linkedin/github/twitter HTTPS values
  }
}
```

`sameAs` preserves contact order and removes later byte-identical duplicates.
Email, phone, location, custom contacts, account data, IDs, hidden values,
entries, employer/school links, storage keys, and owner title are never added.
Sources are the already filtered public projection; no HTML-to-text or ambient
lookup occurs.

Keys serialize in the order shown. Optional keys are omitted, never null.
Serialization is compact RFC 8259 UTF-8 with no insignificant whitespace.
Quotation mark and reverse solidus become `\"` and `\\`; backspace, form feed,
LF, CR, and tab become `\b`, `\f`, `\n`, `\r`, and `\t`. Other U+0000–U+001F
controls use lowercase `\u00xx`. `<`, `>`, `&`, U+2028, and U+2029 become
`\u003c`, `\u003e`, `\u0026`, `\u2028`, and `\u2029`. Solidus and other Unicode
are not escaped. The exact script is:

```text
<script type="application/ld+json">{compact-json}</script>
```

There is no newline inside the element. CSP SHA-256 is computed over the exact
UTF-8 `{compact-json}` text-node bytes and base64-encoded. The response uses
this exact policy, where `<base64>` is that digest:

```text
default-src 'none'; base-uri 'none'; object-src 'none'; frame-ancestors 'none'; form-action 'none'; script-src 'self' 'sha256-<base64>'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; font-src 'self'; connect-src 'self'; manifest-src 'self'; media-src 'none'; worker-src 'none'
```

Go verifies Nuxt emitted that exact one script and no other inline script.
Hydration uses external same-origin code and public JSON; it does not add an
inline payload script. This one response-specific hash source is the only change
from the Phase 3 policy. Nondiscoverable HTML uses that exact base policy with
`script-src 'self'`, no hash source, and no JSON-LD. A nonce,
`script-src 'unsafe-inline'`, and more than one inline script are forbidden.
