# LinkedIn import

A person creates a new resume from their LinkedIn profile data. They download
their data from LinkedIn, pick the file on aboutme, check what was found, and
create the resume. The browser reads the file; it is never uploaded. Only the
resume the person creates is stored.
[ADR 0059](../adr/0059-linkedin-import-in-the-browser.md) records the decision.

Status: proposed. Facts were checked on 2026-09-26. **Verify** marks a fact from
community sources that the build must confirm first.

## What a normal app can get from LinkedIn

| Claim to check                                                                            | Finding                                                                                                                                                                                                                                                                                                                                                                                                                                       | Source   |
| ----------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | -------- |
| Sign-in scopes give only name, email, and photo                                           | Confirmed. The open permissions are `openid`, `profile`, `email`, and `w_member_social` (posting). The ID token and userinfo carry name, given and family name, picture, locale, email, and `email_verified`. The access table says `profile` includes the headline, but no claim carries it.                                                                                                                                                 | [1], [2] |
| Positions, education, and skills need partner programs (Marketing, Talent, or Compliance) | Confirmed, with corrections. Marketing grants advertising and page data, not member profiles. Talent programs (Apply with LinkedIn, Apply Connect, Recruiter System Connect) need an application as an ATS partner. Compliance is closed. Sales Navigator (SNAP) is also partner-only. "Verified on LinkedIn" Plus adds only the current position and the most recent education, needs business development approval, and forbids hiring use. | [2], [3] |
| The Member Data Portability API exists only for EU/EEA members under the DMA              | Confirmed. The member product works only for members in the EEA and Switzerland. The third-party product needs business verification with a business email and legal entity, and "only LinkedIn users from the European Economic Area are allowed to consent". A member in Vietnam cannot use either.                                                                                                                                         | [4], [5] |
| Profile "Save to PDF" can feed an import                                                  | New finding: LinkedIn says Save to PDF "currently supports only English characters", the profile and the member's language setting must be English, and other profiles may render improperly. It is desktop only.                                                                                                                                                                                                                             | [6]      |
| Members can download their own data                                                       | Settings & Privacy → Data privacy → Download your data. Chosen categories arrive by email "within minutes"; the larger archive within 24 hours. The link works for 72 hours. Categories include Profile, Positions, Education, Skills, Languages, Certifications, Projects, and Honors.                                                                                                                                                       | [7]      |

## Import paths

| Path                    | Vietnamese profiles                       | Structure                                                  | Works for a user in Vietnam | Verdict  |
| ----------------------- | ----------------------------------------- | ---------------------------------------------------------- | --------------------------- | -------- |
| (a) Save to PDF upload  | No: English characters only, per LinkedIn | Two-column layout text; headings and order must be guessed | Yes, English profiles only  | Rejected |
| (b) Data export archive | Yes: the member's own text in UTF-8 CSV   | One CSV per section, one row per entry                     | Yes                         | Chosen   |
| (c) Official API        | Only name, email, and photo               | No positions, education, or skills                         | No full-profile API         | Rejected |

Path (a) would also need a layout-guessing parser over PDF text. PDF parsers are
a large attack surface; for example, pdf.js had arbitrary script execution
through crafted fonts before version 4.2.67 (CVE-2024-4367) [8]. Path (c) could
at most prefill the name, which the archive already holds.

### Save to PDF structure

One real English-profile export, inspected locally on 2026-09-26 and never
committed, shows this structure:

- Three US Letter pages (612 × 792 pt), PDF 1.4, tagged, produced by Apache FOP
  2.3, with one embedded subset font (Arial Unicode MS, CID TrueType,
  Identity-H) that has a Unicode map. `pdftotext` extracts clean text.
- Page 1 has two columns. The left column holds "Contact" (phone with a type
  label, email, profile URL with "(LinkedIn)", other sites with a type label),
  "Top Skills" (three skills only), and "Publications". The right column holds
  the name, the headline, the location, and "Summary". Later pages use one
  column. Every page ends with "Page N of M".
- "Experience" lists, per role: company, title, `Month YYYY - Month YYYY`
  followed by a duration such as `(1 year 2 months)`, location, then the
  description. Several roles at one company share one company line, followed by
  a total duration line.
- "Education" lists the school, then `Degree, Field · (Month YYYY - Month YYYY)`
  or `· (YYYY - YYYY)`. The line can wrap inside the date.
- Reading order puts the whole left column before the right one, so a parser
  must split columns by position, not by text order.
- The profile had no Vietnamese letters, so it does not test LinkedIn's
  English-only rule. The font has a Unicode map, but LinkedIn still documents
  improper rendering for other languages.

This confirms that path (a) would be a layout parser tied to one English
template, with only three skills, and with contact data the import avoids. It
stays rejected. The import page recognizes a PDF by its first bytes (`%PDF-`),
never parses it, and tells the person to use the data download.

## Flow

1. `/app/new` offers a third start, "Nhập từ LinkedIn" / "Import from LinkedIn",
   beside the gallery and blank starts (**Owner approval** I2). It opens
   `/app/import/linkedin`. At the resume cap, the page shows the cap message and
   no file picker.
2. The page explains, in the interface language, how to get the file: LinkedIn →
   Settings & Privacy → Data privacy → Download your data → pick Profile,
   Positions, Education, Skills, Languages, Certifications, Projects, and Honors
   → Request archive → open the email link within 72 hours. It says the file
   stays on the device.
3. The person picks the ZIP file, or the CSV files when the browser already
   unzipped it (Safari on macOS does this by default).
4. The browser reads and maps the files, then shows the review screen.
5. The person adjusts the review and presses Create. The browser sends one
   `POST /api/v1/resumes` with `title`, `lng`, and `document`, and opens the
   editor. Nothing is written before Create.

Import only creates a new resume. It never changes an existing one, so there is
no merge rule.

## Reading the file

All parsing is TypeScript in the browser, on the main thread, with async reads.
The app page Content Security Policy keeps `worker-src 'none'`, so there is no
worker. Decompression uses the browser's `DecompressionStream("deflate-raw")`
(Chrome 103, Firefox 113, Safari 16.4). A browser without it can still import
the unzipped CSV files. No new package is added.

A small ZIP reader reads only the end record and the central directory, then
inflates only allowlisted entries:

| Check        | Rule                                                                                                                                                                                                                                                                       |
| ------------ | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| File size    | ZIP at most 16 MiB; each CSV picked directly at most 2 MiB; at most 12 files                                                                                                                                                                                               |
| Container    | One disk; no ZIP64; entry count at most 1,000; the end record found within the last 65,557 bytes                                                                                                                                                                           |
| Entry names  | Exact, case-insensitive match at the archive root: `Profile.csv`, `Positions.csv`, `Education.csv`, `Skills.csv`, `Languages.csv`, `Certifications.csv`, `Projects.csv`, `Honors.csv`. Every other entry is skipped unread. A duplicate allowlisted name rejects the file. |
| Entry format | Method stored or deflate only; no encryption flag; local header must agree with the central directory                                                                                                                                                                      |
| Output bound | Inflation stops the moment one entry passes 2 MiB of output or all entries pass 8 MiB, whatever the declared sizes say; CRC-32 must match                                                                                                                                  |
| Time         | The whole read stops after 10 seconds                                                                                                                                                                                                                                      |
| Text         | UTF-8 with `fatal: true`; a leading byte-order mark is dropped; a file that is not UTF-8 is skipped with a notice                                                                                                                                                          |

The CSV reader follows RFC 4180: comma separator, double-quoted fields with `""`
escapes and embedded line breaks, CRLF or LF. It allows at most 2,000 rows per
file, 64 columns, and 32 KiB per field. It finds the header row among the first
5 rows and looks up columns by trimmed, case-insensitive header name, so column
order and extra columns do not matter. A file without its required columns is
skipped with a notice.

Every value is plain text. It is normalized to Unicode NFC, because some exports
store Vietnamese letters decomposed, and control characters other than line
breaks are removed.

## Mapping

Columns are keyed by header name. Names below come from public parsers of the
export, not from LinkedIn documentation [9] (**Verify**).

| File                 | Columns used                                                        | Resume target                                                                                                                                             |
| -------------------- | ------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `Profile.csv`        | First Name, Last Name, Headline, Summary, Geo Location, Websites    | `fullName`; `headline`; a `profile` section with the summary; a `location` detail; one `website` detail per https site                                    |
| `Positions.csv`      | Company Name, Title, Description, Location, Started On, Finished On | `work`: `employer`, `jobTitle`, `description`, `city` and `country`, `dates`                                                                              |
| `Education.csv`      | School Name, Degree Name, Notes, Activities, Start Date, End Date   | `education`: `school`, `degree`, `description` (notes, then activities), `dates`                                                                          |
| `Skills.csv`         | Name                                                                | `skill`: `name`, no level                                                                                                                                 |
| `Languages.csv`      | Name, Proficiency                                                   | `language`: `name`, `level` 1 to 5 from elementary, limited working, professional working, full professional, native or bilingual; unknown gives no level |
| `Certifications.csv` | Name, Url, Authority, Started On                                    | `certificate`: `title`, `titleLink`, `issuer`, `date`                                                                                                     |
| `Projects.csv`       | Title, Description, Url, Started On, Finished On                    | `project`: `title`, `link`, `description`, `dates`                                                                                                        |
| `Honors.csv`         | Title, Description, Issued On                                       | A `custom` section titled "Giải thưởng" / "Honors and awards": `title`, `description`, `dates` with start and end both the issue date                     |

Never read, even when present: phone numbers, email addresses, birth date,
address, zip code, maiden name, license numbers, connections, messages, and
every other file (**Owner approval** I2). The person adds contact details in the
editor.

The document starts from the same blank document `/app/new` builds for the
default template (`startDocument` in `apps/web/app/templates/`), so section
titles, placement, and customization match a blank resume in the chosen
language. Entries keep the file's order, which is newest first. Each entry gets
a new UUID.

Field rules:

- Text longer than its schema limit is cut at a code point boundary and listed
  in the review. Examples: headline 160 (LinkedIn allows 220), job title and
  employer 160, city and country 120, skill and language name 120.
- A location with a comma splits at the last comma: the right part is the
  country, the left part the city. Without a comma it is the city.
- Descriptions become rich text: HTML special characters are escaped, blank
  lines split paragraphs, and runs of lines starting with `•`, `-`, `*`, or `–`
  become one bullet list. Output stays within the sanitizer allowlist (`p`,
  `ul`, `li`) and under 16 KiB.
- A URL is kept only if it parses as `https`, has no user info, and fits the
  schema's link rule after lowercasing the scheme and host. Profile websites
  look like `[PERSONAL:https://…]` (**Verify**); the label is dropped.
- More than 64 entries in a section keeps the first 64 selected and lists the
  rest unselected.

### Names and language

The review asks for the resume language, Vietnamese or English, defaulting to
the interface language as blank creation does
([ADR 0047](../adr/0047-bilingual-resume-workspace.md)). The language sets the
section titles and the name order: Vietnamese puts Last Name first ("Nguyễn Văn
An"), English puts First Name first. The full name stays editable in the review
because people fill LinkedIn's two name fields in different ways.

The import never translates content. The export holds the member's primary
profile text only (**Verify** whether a secondary-language profile appears).

### Dates

`parseLinkedInDate` returns `{y, m?}` or nothing. It accepts `2020`, `Jan 2020`
and `January 2020` (English month names, any case), `01/2020`, `2020-01`,
`2020-01-15`, and Vietnamese forms `thg 1 2020`, `Th1 2020`, and
`tháng 1 năm 2020`. Years must fall in 1900 to 2100, the schema range.

- Start and end: `{start, end, present: false}`.
- Start and an empty end: `{start, end: null, present: true}`.
- No start, or a start after the end: no `dates`, and a review notice.
- An end in the future stays an end, for example an expected graduation.

## Review screen

The review shows, in the interface language:

- Title (default "CV từ LinkedIn" / "LinkedIn resume"), resume language, full
  name, and headline, all editable.
- One group per found section, with the entry count and a checkbox per entry and
  per group; entries show their main fields as text.
- Notices: skipped files, missing columns, unreadable dates, cut fields, and
  entries over a limit.
- The size meter: Create stays disabled while the request would pass 256 KiB,
  with a note to deselect entries. The document must also pass the generated
  schema validator before Create is enabled.

Values render as text nodes only, never as HTML. Leaving the page drops
everything; the review is not saved.

## Privacy

- The file never leaves the browser. No request carries its bytes, and no
  request goes to another origin.
- aboutme stores only the resume the person creates, like any new resume.
  Nothing records that it came from LinkedIn.
- Unread files, such as messages and connections, which hold other people's
  data, are never inflated.
- The privacy notice adds one item (**Owner approval** I3):
  - en: "LinkedIn import: your browser reads the LinkedIn data file you pick. We
    never receive the file; we store only the resume you create from it."
  - vi: "Nhập từ LinkedIn: trình duyệt của bạn đọc tệp dữ liệu LinkedIn bạn
    chọn. Chúng tôi không nhận tệp này; chúng tôi chỉ lưu CV bạn tạo từ nó."

## Security

- The server trusts nothing from the import. The request goes through the
  existing create route: idempotency key, request limit of 256 KiB, schema
  validation, the Go sanitizer, the resume cap, and rate limits.
- The parser's caps bound memory and time in the person's own tab. A hostile
  file can at worst fail the import.
- No new route, dependency, worker, WebAssembly, or eval, so the Content
  Security Policy does not change.

## Compatibility and size

No schema version, OpenAPI, database, MCP, or Go change. The created resume is
an ordinary current document, so older clients read and edit it as any other.
There is no migration and no loss rule. The import code loads only on its route;
the estimate is under 20 KB compressed.

## Tests

Tests cite this design and ADR 0059. Fixtures are synthetic: names such as
"Nguyễn Văn Mẫu" and "Sample Person", employers such as "Công ty TNHH Ví Dụ" and
"Example Co.", and `example.com` links. No real person's data.

Committed fixtures under `apps/web/test/import/linkedin/fixtures/`:

| Fixture                  | Contents                                                                                                                              |
| ------------------------ | ------------------------------------------------------------------------------------------------------------------------------------- |
| `basic-en.zip`           | All eight files; three positions, one current; two degrees with year-only dates; five skills; two languages; a certificate with a URL |
| `basic-vi.zip`           | The same in Vietnamese, one field stored decomposed (NFD), Vietnamese month forms                                                     |
| `csv-only/`              | The eight CSV files of `basic-vi.zip`, for the unzipped path                                                                          |
| `full-archive-extra.zip` | The allowlisted files plus `messages.csv`, `Connections.csv` with a notes preamble, and a subfolder; none is inflated                 |
| `dates.zip`              | Every accepted date form, unreadable dates, start after end, no start, a future end                                                   |
| `quirks.zip`             | Byte-order mark, CRLF, quoted multiline bullets, embedded quotes, reordered and extra columns, header-only and empty files            |
| `limits.zip`             | 220-character headline, 70 skills, 80 positions, a description over 16 KiB, a total over 256 KiB                                      |
| `injection.zip`          | `<script>`, `<img onerror>`, and `javascript:` URLs in every text and URL column                                                      |
| `save-to-pdf-en.pdf`     | A synthetic Save to PDF look-alike, below; picked as is and renamed to `.zip`                                                         |

`save-to-pdf-en.pdf` is generated, not copied. The committed source is
`fixtures/save-to-pdf-en.fo`, an XSL-FO file that reproduces the structure above
with invented data: US Letter, two columns on page 1, the same headings,
duration and education line shapes, and the "Page N of M" footer. It uses a free
font (Noto Sans) instead of Arial Unicode MS. Apache FOP 2.3 renders it once in
a pinned container, and `fixtures/README.md` records the command. No real PDF or
text from one enters the repository.

Hostile archives are built in the test code, not committed: a deflate bomb that
declares a small size and inflates past 1 GiB, a bomb with an honest huge size,
ZIP64, an encrypted entry, a multi-disk end record, duplicate names,
`../Profile.csv` and absolute names, a truncated file, a bad CRC, 1,001 entries,
an allowlisted name inside a nested ZIP, invalid UTF-8, and a 17 MiB file.

- Unit tests (Vitest): ZIP reader limits and rejections, CSV reader, dates,
  every mapping row, name order, rich text output against the sanitizer
  allowlist, the request size meter, and the schema validator on each fixture's
  output. A bomb must stop within the output cap.
- Component test: the review screen renders injection fixture values as text.
- Browser proof, `deploy/dev-https-browser/linkedin-import.spec.ts`: at 390 and
  1280 px, in Vietnamese and English, pick `basic-vi.zip`, deselect one section,
  create, and land in the editor with the expected content. The proof records
  every request and asserts that none carries the file bytes, the only write is
  one JSON `POST /api/v1/resumes`, and no request leaves the origin. It also
  covers the cap message, the CSV-only path, and the PDF message.

## Facts to confirm before the build

The owner downloads his own data twice, once with LinkedIn set to English and
once to Vietnamese, and reports only the header row of each of the eight files
and the shape of one date value per file, with no values. The fixtures follow
that report. If the headers or date forms differ from this design, the architect
updates the mapping before the build starts.

## Owner approval

| ID  | Choice                                                                                            | Recommendation |
| --- | ------------------------------------------------------------------------------------------------- | -------------- |
| I1  | Import reads the LinkedIn data download, in the browser; no PDF import and no LinkedIn API        | Yes            |
| I2  | Entry on `/app/new`; creates a new resume only; the eight files and fields above; no contact data | Yes            |
| I3  | Privacy notice item, above; the owner reviews the Vietnamese                                      | Yes            |

## Sources

Retrieved 2026-09-26.

1. [Sign In with LinkedIn using OpenID Connect](https://learn.microsoft.com/en-us/linkedin/consumer/integrations/self-serve/sign-in-with-linkedin-v2)
2. [Getting Access to LinkedIn APIs](https://learn.microsoft.com/en-us/linkedin/shared/authentication/getting-access)
3. [Verified on LinkedIn overview](https://learn.microsoft.com/en-us/linkedin/consumer/integrations/verified-on-linkedin/overview)
4. [Member Data Portability (Member)](https://learn.microsoft.com/en-us/linkedin/dma/member-data-portability/member-data-portability-member/)
5. [Member Data Portability (3rd Party)](https://learn.microsoft.com/en-us/linkedin/dma/member-data-portability/member-data-portability-3rd-party/)
6. [Save a profile as a PDF](https://www.linkedin.com/help/linkedin/answer/a541960)
7. [Download your account data](https://www.linkedin.com/help/linkedin/answer/a1339364)
8. [CVE-2024-4367](https://nvd.nist.gov/vuln/detail/CVE-2024-4367)
9. [JMPerez/linkedin-to-json-resume, `src/js/converter.ts`](https://github.com/JMPerez/linkedin-to-json-resume/blob/master/src/js/converter.ts)
   (third-party parser; column positions for Profile, Positions, Education,
   Languages, and Certifications)
