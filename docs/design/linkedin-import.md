# LinkedIn import

A person creates a new resume from their LinkedIn profile. They save their
English profile with LinkedIn's "Save to PDF", pick the file on aboutme, check
what was found, and create the resume. The browser reads the file; it is never
uploaded. Only the resume the person creates is stored.
[ADR 0064](../adr/0064-linkedin-import-from-save-to-pdf.md) records the choice
of the PDF, and [ADR 0059](../adr/0059-linkedin-import-in-the-browser.md) the
rule that parsing stays in the browser.

Status: proposed. Facts were checked on 2026-09-26 and 2026-09-27. **Verify**
marks a fact the build must confirm first, in
[Facts to confirm](#facts-to-confirm-before-the-build).

## Sources a normal app can use

| Source         | Finding                                                                                                                                                                                                           | Source   |
| -------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | -------- |
| Sign-in scopes | `openid`, `profile`, and `email` give name, picture, locale, and email only. No claim carries positions, education, or skills.                                                                                    | [1], [2] |
| Partner APIs   | Positions, education, and skills need Talent, Sales Navigator, or Compliance partner programs, which a small resume service cannot join. Member Data Portability accepts only members in the EEA and Switzerland. | [2], [3] |
| Save to PDF    | Desktop only, one click from the profile. "Currently supports only English characters"; the profile and the member's language setting must be English. Works on other members' profiles too, 200 PDFs a month.    | [4]      |
| Data download  | A ZIP of UTF-8 CSV files, one per section, sent by email; covers Vietnamese profiles. Deferred to a later release (ADR 0064, decision 8).                                                                         | [5]      |

## Save to PDF structure

One real English-profile PDF, inspected locally on 2026-09-26 and never
committed, shows this structure:

- US Letter pages (612 × 792 pt), PDF 1.4, tagged, produced by Apache FOP 2.3,
  with one embedded subset font (Arial Unicode MS, CID TrueType, Identity-H)
  that has a Unicode map. Text extracts cleanly.
- Page 1 has two columns. The left column (the sidebar) holds "Contact" (phone
  with a type label, email, profile URL with "(LinkedIn)", other sites with a
  type label), "Top Skills" (three skills), and "Publications". The right column
  holds the name, the headline, the location, and "Summary". Later pages use one
  column. Every page ends with "Page N of M".
- "Experience" lists, per role: company, title, `Month YYYY - Month YYYY`
  followed by a duration such as `(1 year 2 months)`, location, then the
  description. Several roles at one company share one company line, followed by
  a total duration line.
- "Education" lists the school, then `Degree, Field · (Month YYYY - Month YYYY)`
  or `· (YYYY - YYYY)`. The line can wrap inside the date.
- Reading order puts the whole left column before the right one, so the parser
  splits columns by position, not by text order.

Third-party parsers also name "Languages", "Certifications", and "Honors-Awards"
in the sidebar [6], and PDF metadata with author "LinkedIn" and subject "Resume
generated from profile" [7] (**Verify** both).

## Flow

1. The create dialog on `/app/resumes` gets a link, "Nhập từ LinkedIn (PDF)" /
   "Import from LinkedIn (PDF)", beside "Browse all templates" (**Owner
   approval** I3). It opens `/app/import/linkedin` with a full page load. At the
   resume cap, the page shows the cap message and no file picker.
2. The page explains, in the interface language: open your LinkedIn profile on a
   computer, set LinkedIn's language to English, choose More, then Save to PDF.
   It says that only English profiles work, that the file stays on this device,
   and to import your own profile.
3. The person picks one PDF (`accept="application/pdf,.pdf"`).
4. The browser checks and reads the file, maps it, and shows the review screen,
   or a message that names what is wrong.
5. The person adjusts the review and presses Create. The browser sends one
   `POST /api/v1/resumes` with `title`, `lng`, and `document`, and opens the
   editor. Nothing is written before Create.

Import only creates a new resume, so there is no merge rule. A browser without
module workers, or older than pdf.js's legacy build supports (Chrome 125,
Firefox ESR, Safari 18) [8], gets a message instead of the file picker.

## Reading the file

Parsing is TypeScript in the browser. pdf.js 6.3.289 (legacy build) runs in a
dedicated module worker; the rest runs on the main thread over plain data.
`GlobalWorkerOptions.workerPort` receives a worker built from
`pdfjs-dist/legacy/build/pdf.worker.min.mjs`. The page loads pdf.js and starts a
worker when it opens, so no request follows the pick. Each pick gets a fresh
worker; the page ends the old one. The route renders on the client only
(`ssr: false`) and imports pdf.js dynamically, so no pdf.js code reaches the
server bundle or any other route.

`getDocument` options: `data` (the bytes read), `useWasm: false`,
`enableXfa: false`, `isOffscreenCanvasSupported: false`,
`isImageDecoderSupported: false`, `useSystemFonts: false`,
`disableFontFace: true`, `stopAtErrors: true`, and no `url`, `cMapUrl`,
`standardFontDataUrl`, or `wasmUrl`. An `onPassword` callback rejects the file.
The page calls only `getDocument`, `getMetadata`, `getPage`, and
`getTextContent`; it never renders, and never loads the viewer or its scripting.

| Check         | Rule                                                                                                                                                |
| ------------- | --------------------------------------------------------------------------------------------------------------------------------------------------- |
| Size          | Read `file.slice(0, 4 MiB + 1)`; more than 4 MiB of bytes read rejects the file. The browser's `File.size` is only a hint.                          |
| Header        | `%PDF-` within the first 1,024 bytes read                                                                                                           |
| Encryption    | Any password request rejects the file                                                                                                               |
| Pages         | `numPages` from 1 to 20, checked before any page is read                                                                                            |
| Text          | Items are counted as they arrive; more than 100,000 extracted characters or 20,000 text items in the file stops the read with the too-large message |
| Time          | 15 seconds from the pick; then `loadingTask.destroy()` and `worker.terminate()`                                                                     |
| Readable text | More than 1% of characters that are U+FFFD, private use (U+E000 to U+F8FF), or controls other than line breaks gives "cannot read this file"        |

A failed check shows its message and creates nothing. Every string is normalized
to Unicode NFC after extraction.

### Lines

Each text item has a string, a transform, a width, and a height. The page turns
items into lines per page: items whose baselines lie within 40% of their font
size join one line, ordered left to right, with one space where the gap between
items is wider than a quarter of the font size. A line keeps its page, left
edge, baseline, and font size (the transform's vertical scale), rounded to 0.5
pt.

- **Footer.** A line matching `^Page (\d+) of (\d+)$` in the bottom 72 pt is the
  footer. Every page must have one, with N equal to the page number and M to
  `numPages`. The footer is then dropped.
- **Columns.** On page 1, the name is the line with the largest font size; its
  left edge minus 12 pt is the column boundary B. Lines left of B are sidebar,
  the rest are main. Later pages apply the same B (**Verify** that the sidebar
  can continue to page 2).
- **Headings.** A line is a heading when its whole text equals a known heading
  and its font size is larger than the median line size of the file. A line in
  the font size of the known headings found, that is not one of them, is an
  unknown heading: it ends the section before it, and its section is dropped.

Known headings: sidebar "Contact", "Top Skills", "Languages", "Certifications",
"Honors-Awards", "Publications", "Patents"; main "Summary", "Experience",
"Education", "Volunteer Experience", "Projects" (**Verify** the list). Sidebar
and main lines each join across pages in order, and each section holds the lines
up to the next heading of its column.

### Is it an English LinkedIn PDF?

1. No footer on some page: "This is not a LinkedIn profile PDF. Open your
   profile on LinkedIn, choose More, then Save to PDF."
2. Footers on every page, but fewer than two of "Contact", "Top Skills",
   "Summary", "Experience", "Education": "This LinkedIn PDF is not in English.
   LinkedIn saves only English profiles to PDF. Set LinkedIn's language to
   English and save the PDF again."
3. Otherwise the file is read. Metadata is not required, because LinkedIn can
   change it.

Vietnamese letters that extract cleanly are kept, since a name such as "Nguyễn"
can appear in an English profile. LinkedIn does not promise they render
(**Verify**).

## Mapping

| PDF part       | Rule                                                                                                                                                                          | Resume target                                                                                                                     |
| -------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------- |
| Name           | The largest line on page 1                                                                                                                                                    | `personalDetails.fullName`, as printed                                                                                            |
| Headline       | Main lines after the name, up to the location line or the first heading, joined with spaces                                                                                   | `personalDetails.headline`                                                                                                        |
| Location       | The last main line before the first heading, when its font size is smaller than the headline's (**Verify**)                                                                   | A `location` detail                                                                                                               |
| Contact        | Lines join until one ends with `(Label)`. A value with `@` is an email, digits and `+ ( ) -` a phone, `(LinkedIn)` the profile URL, other labels websites (**Verify** labels) | `email`, `phone`, `linkedin`, and `website` details; a URL gets `https://` and must pass the link rule                            |
| Top Skills     | One skill per entry                                                                                                                                                           | `skill`: `name`, no level                                                                                                         |
| Languages      | `Name (Proficiency)`; lines join until the parentheses close                                                                                                                  | `language`: `name`, `level` 1 to 5 from Elementary, Limited Working, Professional Working, Full Professional, Native or Bilingual |
| Certifications | One title per entry                                                                                                                                                           | `certificate`: `title`                                                                                                            |
| Summary        | Paragraphs and bullets, below                                                                                                                                                 | A `profile` section with one entry                                                                                                |
| Experience     | Entries, below                                                                                                                                                                | `work`: `employer`, `jobTitle`, `dates`, `city` and `country`, `description`                                                      |
| Education      | Entries, below                                                                                                                                                                | `education`: `school`, `degree`, `dates`                                                                                          |

In the sidebar lists, a gap between lines larger than 1.3 times the font size
starts a new entry, so a wrapped skill or certificate stays one entry
(**Verify**).

**Experience.** A date line matches the date range below, optionally followed by
a duration in parentheses. A duration line holds only a duration, such as
`3 years 4 months` or `less than a year` (**Verify** forms). For each date line:

- The line before it is the job title.
- The line before the title is the employer when its font size is the largest in
  the section. When it is a duration line, the line before that is the employer
  of a group, and later roles without their own employer line take the group's
  employer. The group's duration is dropped.
- The line after the date line is the location when its font size equals the
  date line's and it is not a date line (**Verify** sizes); otherwise there is
  no location.
- The description is every line after that, up to the next entry's first line.

**Education.** A school line has the section's largest font size. The lines
after it, up to the next school, join with spaces. The text after the last `·`
is the date part in parentheses; the text before it is the degree, kept as
LinkedIn writes it ("Degree, Field").

**Rich text.** Lines join into paragraphs; a vertical gap larger than 1.5 times
the line height starts a new paragraph. A line starting with `•`, `-`, `*`, or
`–` starts a list item, and a run of them becomes one `ul`. HTML special
characters are escaped. Output uses only `p`, `ul`, and `li` from the sanitizer
allowlist and stays under 16 KiB.

**Locations** split at the last comma: the right part is the country, the left
the city. Without a comma it is the city.

**Limits.** Text longer than its schema limit is cut at a code point boundary
and listed in the review: headline, name, job title, employer, school, degree,
certificate title 160; city, country, skill and language names 120. More than 64
entries in a section keeps the first 64 selected and lists the rest unselected.

The document starts from `blankTemplateDocument` in
`apps/web/app/templates/startDocument.ts` with the preset of the first template
in the gallery order (**Owner approval** I4), fills `content` with the sections
above under English section titles, and then runs `applyTemplate` so placement
covers the new section keys, as a blank resume would. Each entry gets a new
UUID. The resume language is English (`lng: "en"`, **Owner approval** I4); the
import never translates.

### Dates

`parseLinkedInDate` returns `{y, m?}` or nothing. It accepts `2020`, `Jan 2020`,
and `January 2020` (English month names, any case). A range is `start - end`,
with a hyphen or an en dash and spaces (**Verify**), where `end` may be
`Present`. Years must fall in 1900 to 2100, the schema range.

- Start and end: `{start, end, present: false}`.
- Start and `Present`: `{start, end: null, present: true}`.
- Start only: `{start, end: start, present: false}` for education, whose single
  year is a graduation year; a review notice for work.
- Unreadable, or a start after the end: no `dates`, and a review notice.
- Durations in parentheses are dropped; the renderer does not show them.

## What is dropped

- "Honors-Awards", "Publications", "Patents", "Volunteer Experience",
  "Projects", and any other heading: the review lists each one as "Not imported"
  with its entry count, so the person can add them in the editor.
- Durations, the page footers, and the group duration lines.
- The PDF itself, its metadata, and every line outside a mapped section.

## Review screen

The review shows, in the interface language:

- Title (default "CV từ LinkedIn" / "LinkedIn resume") and full name and
  headline, all editable. A line says the resume is in English.
- One group per found section, with the entry count and a checkbox per entry and
  per group; entries show their main fields as text. Email and phone details
  start unselected, because a published resume shows them (**Owner approval**
  I5).
- A line under the groups: "Check each entry. The PDF layout can split or join
  lines; you can fix them in the editor."
- Notices: sections not imported, unreadable dates, cut fields, and entries over
  a limit.
- The size meter: Create stays disabled while the request would pass 256 KiB,
  with a note to deselect entries. The document must also pass the generated
  schema validator before Create is enabled.

Values render as text nodes only, never as HTML. Leaving the page drops
everything; the review is not saved.

## Privacy

- The file never leaves the browser. No request carries its bytes, and pdf.js
  gets no URL to fetch.
- aboutme stores only the resume the person creates, like any new resume.
  Nothing records that it came from LinkedIn.
- The privacy notice adds one item (**Owner approval** I6):
  - en: "LinkedIn import: your browser reads the LinkedIn profile PDF you pick.
    We never receive the file; we store only the resume you create from it."
  - vi: "Nhập từ LinkedIn: trình duyệt của bạn đọc tệp PDF hồ sơ LinkedIn bạn
    chọn. Chúng tôi không nhận tệp này; chúng tôi chỉ lưu CV bạn tạo từ nó."

## Security

- The server trusts nothing from the import. The request goes through the
  existing create route: idempotency key, the 256 KiB request limit, schema
  validation, the Go sanitizer, the resume cap, and rate limits.
- pdf.js runs in a worker with no DOM. Its known script-execution advisories
  (ADR 0064) need font `eval`, PostScript compiled to JavaScript, or viewer
  scripting; 6.3.289 has none of them on this path. WebAssembly is off.
- The app page policy changes `worker-src 'none'` to `worker-src 'self'`
  (**Owner approval** I2). `script-src 'self'` already lets same-origin code
  run, so the change adds no new code source. Public HTML, print, and harness
  policies keep `worker-src 'none'`. The worker script is a same-origin
  `/_nuxt/` file and, under CSP Level 3, runs under its own response's policy,
  the app page policy (**Verify** the header reaches `/_nuxt/` files).
- The size, page, text, and time caps bound time in the person's own tab; the
  time cap terminates the worker. pdf.js has no output cap for compressed
  streams, so a small file whose page content inflates to gigabytes uses worker
  memory until the time cap or the browser's own limit ends the worker. A
  hostile file can at worst fail the import or crash that tab.
- The page never requests a PDF's JavaScript actions, forms, annotations, links,
  or embedded files: text content is the only output used.
- `pdfjs-dist` moves to runtime dependencies at the same exact version; upgrades
  need review against its advisories.

## Compatibility and size

No schema version, OpenAPI, database, MCP, or Go change. The created resume is
an ordinary current document, so older clients read and edit it as any other.
There is no migration and no loss rule.

The import route loads about 450 KB Brotli of pdf.js (126 KB API and 322 KB
worker, measured on 6.3.289) plus the import code, estimated under 25 KB. No
other route loads them.

## Tests

Tests cite this design and ADR 0064. Fixtures are synthetic: names such as
"Sample Person" and "Nguyễn Văn Mẫu", employers such as "Example Co.", and
`example.com` links. No real person's PDF or text from one enters the
repository.

Layout fixtures under `apps/web/test/import/linkedin/fixtures/` are generated,
not copied. Each has a committed XSL-FO source (`*.fo`) that reproduces the
structure above with invented data, rendered once by Apache FOP 2.3 in a pinned
container with a free font (Noto Sans) and a Unicode map. `fixtures/README.md`
records the command, and the generated PDFs are committed.

| Fixture                  | Contents                                                                                                                                                  |
| ------------------------ | --------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `basic-en.pdf`           | 3 pages; sidebar with all contact kinds, three skills, two languages, two certificates; summary with bullets; four roles, two at one company; two schools |
| `wraps-en.pdf`           | A wrapped headline, profile URL, language, certificate, and education date line; an entry split across a page break; a sidebar continuing on page 2       |
| `dates-en.pdf`           | Every accepted date form, `Present`, year only, an unreadable date, a start after the end, `less than a year`                                             |
| `dropped-en.pdf`         | Honors-Awards, Publications, Patents, Volunteer Experience, and Projects sections                                                                         |
| `vietnamese-letters.pdf` | An English profile whose name and one employer carry Vietnamese letters                                                                                   |
| `localized-vi.pdf`       | The same layout with Vietnamese headings: the English-only message                                                                                        |
| `limits-en.pdf`          | 20 pages; a 220-character headline; 80 roles; a description over 16 KiB                                                                                   |
| `injection-en.pdf`       | `<script>`, `<img onerror>`, and `javascript:` text in every mapped field and contact line                                                                |
| `other.pdf`              | A two-page document without LinkedIn footers: the not-LinkedIn message                                                                                    |

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
  fixtures cannot, such as missing locations and unknown headings.
- CSP tests: `apps/web/test/csp.test.ts` and `apps/web/e2e/normal-csp.spec.ts`
  assert the new app policy; the public, print, and harness policies stay
  unchanged.
- Component test: the review renders `injection-en.pdf` values as text.
- Browser proof, `deploy/dev-https-browser/linkedin-import.spec.ts`: at 390 and
  1280 px, in Vietnamese and English, pick `basic-en.pdf`, deselect one section,
  create, and land in the editor with the expected content. It records every
  request and asserts that none carries the file bytes, the only write is one
  JSON `POST /api/v1/resumes`, no request leaves the origin, and no request
  happens between the pick and Create. It also covers the cap message, the
  not-LinkedIn and English-only messages, and the time limit on a slow file in
  Chromium and WebKit.

## Facts to confirm before the build

The frontend first adds `apps/web/scripts/linkedin-pdf-shape.mjs`. It reads a
PDF with the same pdf.js options and prints only its shape: page count, metadata
producer, author, and subject, and per line the page, column, left edge, font
size, and either the heading text, `DATE`, `DURATION`, `FOOTER`, or `TEXT(n)`,
with letters replaced by `a` and digits by `9`. It prints no value from the
profile. The owner runs it on his own English Save to PDF and sends the output.
It settles:

1. The heading list, their order, and the sidebar and main split.
2. Font sizes for name, headline, location, heading, employer, title, date,
   location, and body lines.
3. Whether the sidebar continues on later pages, and the column boundary.
4. Date separators, month name forms, `Present`, and duration forms.
5. Contact type labels, and whether long URLs wrap.
6. Metadata, and that the file has no encryption.
7. Whether a long sidebar entry wraps with a smaller gap than between entries.

If a fact differs from this design, the architect updates it before the mapping
code starts. Two facts stay open after that: whether Vietnamese letters in an
English profile extract cleanly (the owner's profile has none), and whether
Nuxt's route header reaches `/_nuxt/` files, which the CSP test checks.

## Owner approval

| ID  | Choice                                                                                                                               | Recommendation                                                    |
| --- | ------------------------------------------------------------------------------------------------------------------------------------ | ----------------------------------------------------------------- |
| I1  | Accept ADR 0064: English Save to PDF, parsed in the browser by pdf.js 6.3.289 in a worker; `pdfjs-dist` becomes a runtime dependency | Yes                                                               |
| I2  | App page policy `worker-src 'self'`; public, print, and harness policies unchanged                                                   | Yes. Fallback: main-thread pdf.js with no hard time limit         |
| I3  | Entry link in the create dialog; page `/app/import/linkedin`                                                                         | Yes                                                               |
| I4  | Resume language English; the gallery's first template; the person changes the template in the editor                                 | Yes                                                               |
| I5  | Sections and fields in Mapping; email and phone unselected; the dropped list above                                                   | Yes                                                               |
| I6  | Privacy notice item above; the owner reviews the Vietnamese                                                                          | Yes                                                               |
| I7  | Non-LinkedIn and non-English PDFs get a message and no import; no partial import                                                     | Yes                                                               |
| I8  | The page asks people to import their own profile; no technical check                                                                 | Yes. A name check against the account would reject real users     |
| I9  | Browsers older than Chrome 125, Firefox ESR, and Safari 18 get a message                                                             | Yes. Save to PDF is desktop-only, where these versions are common |
| I10 | The data download import stays deferred as the later path for Vietnamese profiles                                                    | Yes. Decide once the PDF import is in use                         |

## Sources

Retrieved 2026-09-26 and 2026-09-27.

1. [Sign In with LinkedIn using OpenID Connect](https://learn.microsoft.com/en-us/linkedin/consumer/integrations/self-serve/sign-in-with-linkedin-v2)
2. [Getting Access to LinkedIn APIs](https://learn.microsoft.com/en-us/linkedin/shared/authentication/getting-access)
3. [Member Data Portability (Member)](https://learn.microsoft.com/en-us/linkedin/dma/member-data-portability/member-data-portability-member/)
4. [LinkedIn Help: Save a profile as a PDF](https://www.linkedin.com/help/linkedin/answer/a541960)
5. [LinkedIn Help: Download your account data](https://www.linkedin.com/help/linkedin/answer/a1339364)
6. [ez-parse, a LinkedIn PDF parser](https://github.com/ShivanshSrivastava1/ez-parse)
   (third party; section headings)
7. [clickfolio.me pull request 293](https://github.com/Divkix/clickfolio.me/pull/293)
   (third party; PDF metadata and footers)
8. [pdf.js FAQ: browser support](https://github.com/mozilla/pdf.js/wiki/Frequently-Asked-Questions)
