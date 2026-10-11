# LinkedIn import

A person creates a new resume from their LinkedIn profile. They save their
English profile with LinkedIn's "Save to PDF", pick the file on aboutme, check
what was found, and create the resume. The browser reads the file; it is never
uploaded. Only the resume the person creates is stored.
[ADR 0023](../adr/0023-linkedin-import.md) records the choice of the PDF and the
rule that parsing stays in the browser.

Status: accepted and built; the owner approved I1 to I10 on 2026-09-27. Facts
were checked on 2026-09-26 and 2026-09-27. **Verify** marks a fact one profile
could not settle, as listed in
[Open facts](linkedin-import-verification.md#open-facts).

[Parser](linkedin-import-parser.md) defines extraction and mapping;
[verification](linkedin-import-verification.md) covers fixtures and
compatibility.

## Sources a normal app can use

| Source         | Finding                                                                                                                                                                                                           | Source   |
| -------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | -------- |
| Sign-in scopes | `openid`, `profile`, and `email` give name, picture, locale, and email only. No claim carries positions, education, or skills.                                                                                    | [1], [2] |
| Partner APIs   | Positions, education, and skills need Talent, Sales Navigator, or Compliance partner programs, which a small resume service cannot join. Member Data Portability accepts only members in the EEA and Switzerland. | [2], [3] |
| Save to PDF    | Desktop only, one click from the profile. "Currently supports only English characters"; the profile and the member's language setting must be English. Works on other members' profiles too, 200 PDFs a month.    | [4]      |
| Data download  | A ZIP of UTF-8 CSV files, one per section, sent by email; covers Vietnamese profiles. Deferred to a later release (ADR 0023, decision 8).                                                                         | [5]      |

## Save to PDF structure

See [Save to PDF structure](linkedin-import-parser.md#save-to-pdf-structure).

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

See [Reading the file](linkedin-import-parser.md#reading-the-file).

### Lines

See [Lines](linkedin-import-parser.md#lines).

### Is it an English LinkedIn PDF?

See
[Is it an English LinkedIn PDF?](linkedin-import-parser.md#is-it-an-english-linkedin-pdf).

## Mapping

See [Mapping](linkedin-import-parser.md#mapping).

### Dates

See [Dates](linkedin-import-parser.md#dates).

## What is dropped

See [What is dropped](linkedin-import-parser.md#what-is-dropped).

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

See [parser security](linkedin-import-parser.md#security).

## Compatibility and size

See
[Compatibility and size](linkedin-import-verification.md#compatibility-and-size).

## Tests

See [Tests](linkedin-import-verification.md#tests).

## Open facts

See [Open facts](linkedin-import-verification.md#open-facts).

## Owner approval

| ID  | Choice                                                                                                                                                         | Decision                             |
| --- | -------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------ |
| I1  | Accept ADR 0023: English Save to PDF, parsed in the browser by pdf.js 6.3.289 in a worker; `pdfjs-dist` becomes a runtime dependency                           | Approved                             |
| I2  | App page policy `worker-src 'self'`; public, print, and harness policies unchanged                                                                             | Approved                             |
| I3  | Entry link in the create dialog; page `/app/import/linkedin`                                                                                                   | Approved                             |
| I4  | Resume language English; the gallery's first template; the person changes the template in the editor                                                           | Approved                             |
| I5  | Sections and fields in [Mapping](linkedin-import-parser.md#mapping); email and phone unselected; the [dropped list](linkedin-import-parser.md#what-is-dropped) | Approved                             |
| I6  | Privacy notice item above; the owner reviews the Vietnamese                                                                                                    | Approved                             |
| I7  | Non-LinkedIn and non-English PDFs get a message and no import; no partial import                                                                               | Approved                             |
| I8  | The page asks people to import their own profile; no technical check                                                                                           | Approved                             |
| I9  | Browsers older than Chrome 125, Firefox ESR, and Safari 18 get a message                                                                                       | Approved; the check needs Chrome 126 |
| I10 | The data download import stays deferred as the later path for Vietnamese profiles                                                                              | Approved                             |

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
