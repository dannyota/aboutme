# LinkedIn import

A person creates a new resume from their LinkedIn profile. They save their
English profile with LinkedIn's "Save to PDF", pick the file on aboutme, check
what was found, and create the resume. The browser reads the file; it is never
uploaded. Only the resume the person creates is stored.
[ADR 0064](../adr/0064-linkedin-import-from-save-to-pdf.md) records the choice
of the PDF, and [ADR 0059](../adr/0059-linkedin-import-in-the-browser.md) the
rule that parsing stays in the browser.

Status: accepted and built; the owner approved I1 to I10 on 2026-09-27. Facts
were checked on 2026-09-26 and 2026-09-27. **Verify** marks a fact one profile
could not settle, as listed in [Open facts](#open-facts).

## Sources a normal app can use

| Source         | Finding                                                                                                                                                                                                           | Source   |
| -------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | -------- |
| Sign-in scopes | `openid`, `profile`, and `email` give name, picture, locale, and email only. No claim carries positions, education, or skills.                                                                                    | [1], [2] |
| Partner APIs   | Positions, education, and skills need Talent, Sales Navigator, or Compliance partner programs, which a small resume service cannot join. Member Data Portability accepts only members in the EEA and Switzerland. | [2], [3] |
| Save to PDF    | Desktop only, one click from the profile. "Currently supports only English characters"; the profile and the member's language setting must be English. Works on other members' profiles too, 200 PDFs a month.    | [4]      |
| Data download  | A ZIP of UTF-8 CSV files, one per section, sent by email; covers Vietnamese profiles. Deferred to a later release (ADR 0064, decision 8).                                                                         | [5]      |

## Save to PDF structure

One English-profile PDF inspected locally on 2026-09-26 and the owner's masked
shape report, neither committed, show this structure:

- US Letter pages (612 × 792 pt), PDF 1.4, tagged (PDF/UA), not encrypted,
  produced by Apache FOP 2.3, with one embedded subset font (Arial Unicode MS,
  CID TrueType, Identity-H) that has a Unicode map. Text extracts cleanly. Every
  line uses the same font resource, so the parser cannot tell bold from normal
  and relies on size, position, and gaps only.
- Page 1 has two columns: the sidebar at x = 21.6 pt and the main column at x =
  223.6 pt. The sidebar holds "Contact", "Top Skills", and "Publications"; the
  main column holds the name, the headline, the location, "Summary",
  "Experience", and "Education", in that order. Later pages print main lines at
  the same left edge; the sidebar ends on page 1. Every page ends with "Page N
  of M" at 9 pt.
- "Contact" prints a phone with its label on the same line
  (`0900000000 (Home)`), then the email alone with no label, then the profile
  URL without a scheme followed by `(LinkedIn)` on its own line, then each other
  site followed by its label on its own line.
- "Experience" lists, per role: company, title,
  `Month YYYY - Month YYYY (1 year 2 months)`, location, then the description.
  Several roles at one company share one company line followed by a total
  duration line without parentheses, such as `2 years 1 month`; each role in the
  group starts at its title. A role can continue on the next page.
- "Education" lists the school, then `Degree, Field · (Month YYYY - Month YYYY)`
  or `Degree, Field · (YYYY - YYYY)`. The line can wrap inside the date.
- Reading order puts the whole left column before the right one, so the parser
  splits columns by position, not by text order.

Measured sizes and baseline gaps, in points. The parser uses relative rules;
fixtures reproduce these values.

| Lines                             | Size      | Gap before the line                                         |
| --------------------------------- | --------- | ----------------------------------------------------------- |
| Name                              | 26        | none; first main line                                       |
| Headline, then location           | 12        | 21 after the name, 15.5 between them                        |
| Sidebar / main heading            | 13 / 16   | 34.5 to 54 after the previous section                       |
| First line under a heading        | any       | 19.5 in the sidebar, 25.5 to 30.5 in main                   |
| Summary body                      | 12        | 18 per line                                                 |
| Employer, school                  | 12        | 38.5 after the previous role, 33.5 after a school           |
| Group duration line               | 10.5      | 16.5 after the employer                                     |
| Job title                         | 11.5      | 16 after the employer, 35 after a grouped role              |
| Date, then role location          | 10.5      | 14.5 each                                                   |
| Description, degree line          | 10.5      | 21.5 after the location, 17.5 after the school, 18 per line |
| Skill, publication                | 10.5      | 12.5 for a wrapped line, 17.5 for a new entry               |
| Contact phone, email / URL, label | 10.5 / 11 | 12.5 to 24.5, not tied to entries                           |

Third-party parsers also name "Languages", "Certifications", and "Honors-Awards"
in the sidebar [6] (**Verify**), and metadata with author "LinkedIn" and subject
"Resume generated from profile" [7]; the report confirms the author only.

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
module workers, `ReadableStream`, or `URL.parse` gets a message instead of the
picker. `URL.parse` ships in Chrome 126, Firefox 126, and Safari 18, one Chrome
version above pdf.js's legacy build floor [8] (**Owner approval** I9).

## Reading the file

Parsing is TypeScript in the browser. pdf.js 6.3.289 (legacy build) runs in a
dedicated module worker; the rest runs on the main thread over plain data.
`GlobalWorkerOptions.workerPort` receives a worker built from
`pdfjs-dist/legacy/build/pdf.worker.min.mjs`. The route renders on the client
only (`ssr: false`) and imports pdf.js dynamically, so no pdf.js code reaches
the server bundle or any other route.

Each pick uses its own worker, started when the page enters the pick state: on
open, after a failed or stopped read, and on "choose another file". Each read
terminates its worker, and none runs during the review. So no request happens
between a pick and its result, or between a pick that reaches the review and
Create; after a failure, the next worker's script loads.

`getDocument` options: `data` (the bytes read), `useWasm: false`,
`enableXfa: false`, `isOffscreenCanvasSupported: false`,
`isImageDecoderSupported: false`, `useSystemFonts: false`,
`disableFontFace: true`, `stopAtErrors: true`, and no `url`, `cMapUrl`,
`standardFontDataUrl`, or `wasmUrl`. `onPassword` is set on the loading task,
not passed as an option (6.3.289 ignores it there), and rejects the file. The
page calls only `getDocument`, `getPage`, and `streamTextContent`, so a read can
stop inside a page. It never renders, reads metadata, or loads the viewer.

| Check         | Rule                                                                                                                                                |
| ------------- | --------------------------------------------------------------------------------------------------------------------------------------------------- |
| Size          | Read `file.slice(0, 4 MiB + 1)`; more than 4 MiB of bytes read rejects the file. The browser's `File.size` is only a hint.                          |
| Header        | `%PDF-` within the first 1,024 bytes read                                                                                                           |
| Encryption    | Any password request rejects the file                                                                                                               |
| Pages         | `numPages` from 1 to 20, checked before any page is read                                                                                            |
| Text          | Items are counted as they arrive; more than 100,000 extracted characters or 20,000 text items in the file stops the read with the too-large message |
| Time          | 15 seconds from the pick, or Stop; then `worker.terminate()` and `loadingTask.destroy()`                                                            |
| Readable text | More than 1% of characters that are U+FFFD, private use (U+E000 to U+F8FF), or controls other than line breaks gives "cannot read this file"        |

A failed check shows its message and creates nothing. Every string is normalized
to Unicode NFC after extraction.

### Lines

Each text item has a string, a transform, a width, and a height. First, the page
finds the column boundary B: the left edge of page 1's largest item (the name)
minus 12 pt. On every page it splits items at B, then within each side joins
items whose baselines lie within 40% of the larger font size into one line,
ordered left to right, with one space where the gap between items is wider than
a quarter of the font size. A sidebar and a main item on one baseline never
join. A line keeps its page, left edge, baseline, and font size (the transform's
vertical scale), rounded to 0.5 pt.

- **Footer.** A line matching `^Page (\d+) of (\d+)$` in the bottom 72 pt is the
  footer. Every page must have one, with N equal to the page number and M to
  `numPages`. The footer is then dropped.
- **Columns.** Lines left of B are sidebar, the rest are main, on every page;
  later pages keep the page-1 main edge. A sidebar that continues on page 2 is
  read the same way (**Verify**; not yet seen).
- **Headings.** Sidebar and main headings differ in size. A line is a heading
  when its whole text equals a known heading of its column and its font size is
  larger than the median line size of the file. The largest such size in a
  column is that column's heading size; a known heading text at a smaller size,
  such as an employer named "Experience" at the employer size, is ordinary text.
  A line at the column's heading size that is not a known heading is an unknown
  heading: it ends the section before it, and its section is dropped.

Known headings: sidebar "Contact", "Top Skills", "Languages", "Certifications",
"Honors-Awards", "Publications", "Patents"; main "Summary", "Experience",
"Education", "Volunteer Experience", "Projects" (**Verify** those the report did
not show). Sidebar and main lines each join across pages in order, and each
section holds the lines up to the next heading of its column. The first line of
a later page has no gap: in the main column it continues the paragraph before
it, and in the sidebar it starts a new entry.

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

| PDF part       | Rule                                                                                                             | Resume target                                                                                                                     |
| -------------- | ---------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------- |
| Name           | The largest line on page 1                                                                                       | `personalDetails.fullName`, as printed                                                                                            |
| Headline       | Main lines after the name, up to the location line or the first heading, joined with spaces                      | `personalDetails.headline`                                                                                                        |
| Location       | The last main line before the first heading, when two or more lines lie between the name and that heading; below | A `location` detail                                                                                                               |
| Contact        | Entries, below                                                                                                   | `email`, `phone`, `linkedin`, and `website` details; a URL gets `https://` and must pass the link rule                            |
| Top Skills     | One skill per entry                                                                                              | `skill`: `name`, no level                                                                                                         |
| Languages      | `Name (Proficiency)`; lines join until the parentheses close                                                     | `language`: `name`, `level` 1 to 5 from Elementary, Limited Working, Professional Working, Full Professional, Native or Bilingual |
| Certifications | One title per entry                                                                                              | `certificate`: `title`                                                                                                            |
| Summary        | Paragraphs and bullets, below                                                                                    | A `profile` section with one entry                                                                                                |
| Experience     | Entries, below                                                                                                   | `work`: `employer`, `jobTitle`, `dates`, `city` and `country`, `description`                                                      |
| Education      | Entries, below                                                                                                   | `education`: `school`, `degree`, `dates`                                                                                          |

**Location.** The headline and the location share one size and line gap, so a
wrapped headline without a location would give its last line as the location
(**Verify** that LinkedIn always prints one). The person can deselect it.

**Contact.** A line with `@` and no label is an email entry on its own. Other
lines join without spaces until a line ends with `(Label)` or is only `(Label)`;
the text before the label is the value. A value of only digits, spaces, and
`+ ( ) -` is a phone, and a value with `@` is an email, whatever its label.
`(LinkedIn)` marks the profile URL; any other label marks a website. A URL with
whitespace, a user name or password, or a host without a dot is not imported;
`http://` becomes `https://`. Gaps do not separate contact entries (**Verify**
other labels and long URL wraps).

In the other sidebar lists, a gap between lines larger than 1.4 times the font
size starts a new entry, so a wrapped skill or certificate stays one entry; 1.4
is the midpoint of the measured wrap and entry gaps (1.19 and 1.67 times).

**Experience.** A date line matches the date range below, optionally followed by
a duration in parentheses: `(1 year 2 months)`, `(1 year)`, or `(9 months)`. A
duration line holds only a duration without parentheses, such as
`2 years 1 month`; each unit comes singular or plural. `less than a year` is
accepted in both places (**Verify**; the report has none). For each date line:

- The job title is the run of lines just before it that share the size of the
  line right before it (11.5 pt in the report), joined with spaces (**Verify**
  that a long title wraps into such a run).
- The line before the title is the employer when its font size is the largest in
  the section. When it is a duration line, the line before that is the employer
  of a group, and later roles without their own employer line take the group's
  employer. The group's duration is dropped.
- The line after the date line is the location when its font size equals the
  date line's, it is not a date line, and its gap is at most 1.6 times its size.
  Otherwise there is no location (**Verify** the gap when a role has none).
- The description is every line after that, up to the next entry's first line.

**Education.** A school line has the section's largest font size; consecutive
school-size lines join as one school. The lines after it, up to the next school,
join with spaces. The text after the last `·` is the date part in parentheses;
the text before it is the degree, kept as LinkedIn writes it ("Degree, Field").

**Rich text.** Lines join into paragraphs with one space. A gap larger than 1.3
times the section's most common line gap starts a new paragraph. The report
shows no such gap, and one sentence end runs into the next capital with no
space, so the PDF likely drops the profile's own line breaks (assumption); the
import keeps the text as extracted. A line starting with `•`, `-`, `*`, or `–`
starts a list item, and a run of them becomes one `ul` (**Verify**; the report
has no bullet line). Only `&`, `<`, and `>` are escaped; quotes stay literal, as
the sanitizer writes them, so the sanitizer returns the output unchanged. Output
uses only `p`, `ul`, and `li` from the allowlist and stays under 16 KiB.

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
with a hyphen or an en dash and spaces, where `end` may be `Present` (**Verify**
`Present` and short names). Years must fall in 1900 to 2100, the schema range.

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
  with a note to deselect entries. Create is enabled only when the document
  passes the generated schema validator and the store checks the create route
  applies (`packages/schema/validation/store.ts`), such as date order.

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
- The caps bound time in the person's own tab. The main thread counts text as it
  arrives, so it cannot stop pdf.js inside the worker, and pdf.js has no output
  cap for compressed streams. A page that inflates to gigabytes uses worker
  memory until a cap, the time limit, or Stop terminates the worker (before
  `destroy()`, which a busy worker may never answer), as the browser proof
  shows. A hostile file can at worst fail the import or crash that tab.
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

Tests cite this design and ADR 0064. Fixtures are synthetic ("Sample Person",
"Nguyễn Văn Mẫu", "Example Co.", `example.com`); no real person's PDF or text
from one enters the repository. Layout fixtures under
`apps/web/test/import/linkedin/fixtures/` are generated, not copied. Each has a
committed XSL-FO source (`*.fo`) that reproduces the structure, sizes, and gaps
above with invented data, rendered once by Apache FOP 2.3 in a pinned container
with a free font (Noto Sans) and a Unicode map. FOP 2.3 cannot read Noto Sans's
GDEF table, so it runs with `-nocs` (no complex script features).
`fixtures/README.md` records the command, and the generated PDFs are committed.

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
  rule in [Reading the file](#reading-the-file) holds. It also covers the cap
  message, the not-LinkedIn and English-only messages, and the time limit on a
  slow file in Chromium and WebKit.

## Open facts

One profile's masked shape report could not settle these facts. The parser
follows the rules above, and the review lets the person fix the result.

| Area     | Open                                                                       |
| -------- | -------------------------------------------------------------------------- |
| Headings | The six known headings absent from that profile                            |
| Sidebar  | A sidebar that continues past page 1                                       |
| Dates    | `Present`, short month names, `less than a year`                           |
| Contact  | Labels other than `(Home)`, `(LinkedIn)`, and one website label; URL wraps |
| Metadata | The Subject value; the parser does not use metadata                        |
| Letters  | Whether Vietnamese letters in an English profile extract cleanly           |
| CSP      | Whether the app page policy header reaches `/_nuxt/` files                 |

## Owner approval

| ID  | Choice                                                                                                                               | Decision                             |
| --- | ------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------ |
| I1  | Accept ADR 0064: English Save to PDF, parsed in the browser by pdf.js 6.3.289 in a worker; `pdfjs-dist` becomes a runtime dependency | Approved                             |
| I2  | App page policy `worker-src 'self'`; public, print, and harness policies unchanged                                                   | Approved                             |
| I3  | Entry link in the create dialog; page `/app/import/linkedin`                                                                         | Approved                             |
| I4  | Resume language English; the gallery's first template; the person changes the template in the editor                                 | Approved                             |
| I5  | Sections and fields in Mapping; email and phone unselected; the dropped list above                                                   | Approved                             |
| I6  | Privacy notice item above; the owner reviews the Vietnamese                                                                          | Approved                             |
| I7  | Non-LinkedIn and non-English PDFs get a message and no import; no partial import                                                     | Approved                             |
| I8  | The page asks people to import their own profile; no technical check                                                                 | Approved                             |
| I9  | Browsers older than Chrome 125, Firefox ESR, and Safari 18 get a message                                                             | Approved; the check needs Chrome 126 |
| I10 | The data download import stays deferred as the later path for Vietnamese profiles                                                    | Approved                             |

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
