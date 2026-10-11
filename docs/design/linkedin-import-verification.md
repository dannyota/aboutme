# LinkedIn import verification

[LinkedIn import](linkedin-import.md) owns the flow, privacy promises, and
[owner approval](linkedin-import.md#owner-approval).
[The parser](linkedin-import-parser.md) owns extraction and mapping.

## Compatibility and size

No schema version, OpenAPI, database, MCP, or Go change. The created resume is
an ordinary current document, so older clients read and edit it as any other.
There is no migration and no loss rule.

The import route loads about 450 KB Brotli of pdf.js (126 KB API and 322 KB
worker, measured on 6.3.289) plus the import code, estimated under 25 KB. No
other route loads them.

## Tests

Tests cite this design and ADR 0023. Fixtures are synthetic ("Sample Person",
"Nguyễn Văn Mẫu", "Example Co.", `example.com`); no real person's PDF or text
from one enters the repository. Layout fixtures under
`apps/web/test/import/linkedin/fixtures/` are generated, not copied. Each has a
committed XSL-FO source (`*.fo`) that reproduces the structure, sizes, and gaps
in [Save to PDF structure](linkedin-import-parser.md#save-to-pdf-structure) with
invented data, rendered once by Apache FOP 2.3 in a pinned container with a free
font (Noto Sans) and a Unicode map. FOP 2.3 cannot read Noto Sans's GDEF table,
so it runs with `-nocs` (no complex script features). `fixtures/README.md`
records the command, and the generated PDFs are committed.

| Fixture                  | Contents                                                                                                                                                                                                                                                                                                          |
| ------------------------ | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `basic-en.pdf`           | 3 pages; sidebar with a labeled phone, an unlabeled email, a profile URL and a website each labeled on the next line, three skills, two languages, two certificates; summary with bullets; four roles, two at one company under a duration line, one continuing on page 2; two schools, one with its date wrapped |
| `wraps-en.pdf`           | A wrapped headline with a location; a wrapped profile URL, skill, language, certificate, and job title; a sidebar continuing on page 2                                                                                                                                                                            |
| `dates-en.pdf`           | Every accepted date form, `Present`, year only, an unreadable date, a start after the end, `less than a year` as a date duration and as a group duration                                                                                                                                                          |
| `dropped-en.pdf`         | Honors-Awards, Publications, Patents, Volunteer Experience, and Projects sections                                                                                                                                                                                                                                 |
| `vietnamese-letters.pdf` | An English profile whose name and one employer carry Vietnamese letters                                                                                                                                                                                                                                           |
| `localized-vi.pdf`       | The same layout with Vietnamese headings: the English-only message                                                                                                                                                                                                                                                |
| `limits-en.pdf`          | 20 pages; a 220-character headline; 80 roles; a description over 16 KiB                                                                                                                                                                                                                                           |
| `injection-en.pdf`       | `<script>`, `<img onerror>`, and `javascript:` text in every mapped field and contact line                                                                                                                                                                                                                        |
| `other.pdf`              | A two-page document without LinkedIn footers: the not-LinkedIn message                                                                                                                                                                                                                                            |

Hostile files are built in test code, not committed: 4 MiB + 1 byte, no `%PDF-`
header, a truncated file, an encrypted file, 21 pages, text past 100,000
characters, a font without a Unicode map (unreadable text), an `OpenAction`
JavaScript action, an XFA form, a Type 4 (PostScript) function, a page content
stream that inflates past 1 GiB, and deeply nested objects.

- Unit tests (Vitest, pdf.js legacy build in Node, as the browser tests already
  use it): each check and message; line building, footers, columns, headings;
  every mapping row; dates; rich text against the sanitizer allowlist; the size
  meter; the schema validator on each fixture's output; the `getDocument`
  options; and a scan of the pinned pdf.js files for `eval` and `Function`
  calls.
- A mapping test over line arrays (JSON, synthetic) covers layouts the FOP
  fixtures cannot, such as a wrapped headline without a location, a role without
  a location, and unknown headings.
- CSP tests: `apps/web/test/csp.test.ts` and `apps/web/e2e/normal-csp.spec.ts`
  assert the app policy and the unchanged public, print, and harness policy.
- Component test: the review renders `injection-en.pdf` values as text.
- Browser proof, `deploy/dev-https-browser/linkedin-import.spec.ts`: at 390 and
  1280 px, in Vietnamese and English, pick `basic-en.pdf`, deselect one section,
  create, and land in the editor with the expected content. It records every
  request and asserts that none carries the file bytes, the only write is one
  JSON `POST /api/v1/resumes`, no request leaves the origin, and the request
  rule in [Reading the file](linkedin-import-parser.md#reading-the-file) holds.
  It also covers the cap message, the not-LinkedIn and English-only messages,
  and the time limit on a slow file in Chromium and WebKit.

## Open facts

One profile's masked shape report could not settle these facts. The parser
follows the [parser rules](linkedin-import-parser.md), and the review lets the
person fix the result.

| Area     | Open                                                                       |
| -------- | -------------------------------------------------------------------------- |
| Headings | The six known headings absent from that profile                            |
| Sidebar  | A sidebar that continues past page 1                                       |
| Dates    | `Present`, short month names, `less than a year`                           |
| Contact  | Labels other than `(Home)`, `(LinkedIn)`, and one website label; URL wraps |
| Metadata | The Subject value; the parser does not use metadata                        |
| Letters  | Whether Vietnamese letters in an English profile extract cleanly           |
| CSP      | Whether the app page policy header reaches `/_nuxt/` files                 |
