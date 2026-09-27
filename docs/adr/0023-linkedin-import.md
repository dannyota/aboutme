# 0023: LinkedIn import reads the English Save to PDF in the browser

Status: Accepted (2026-09-27). The owner chose Save to PDF as the first import
source and approved the choices marked **Owner approval** in
[LinkedIn import](../design/linkedin-import.md#owner-approval) on 2026-09-27.

## Context

People want to start a resume from their LinkedIn profile. LinkedIn's open API
permissions return only name, email, photo, and locale. Positions, education,
and skills need partner programs a small resume service cannot join, and the
Member Data Portability API accepts only members in the EEA and Switzerland.
Most aboutme users are in Vietnam.

Two files remain that a member can get alone. The data download is a ZIP of
UTF-8 CSV files, one per section, sent by email; it covers Vietnamese profiles
but also holds other people's messages and connections. Profile "Save to PDF"
takes one click on desktop, but LinkedIn's help page says it "currently supports
only English characters" and that the profile and language setting must be
English [1]. The same page lets a member save other members' profiles too, up to
200 PDFs a month [1].

One real English-profile PDF, inspected locally and never committed, is PDF 1.4
from Apache FOP 2.3 with one embedded subset font that has a Unicode map, a
two-column first page, and a "Page N of M" footer on every page. Its text
extracts cleanly, but only by position.

A PDF parser is a large attack surface. pdf.js has three advisories that let a
crafted PDF run script:

| Advisory                | Cause                                                            | Fixed in |
| ----------------------- | ---------------------------------------------------------------- | -------- |
| CVE-2018-5158 [2]       | PostScript calculator functions compiled to JavaScript           | 2.0.943  |
| CVE-2024-4367 [3]       | Font code compiled with `eval` while `isEvalSupported` was true  | 4.2.67   |
| CVE-2026-16633 [4], [5] | Viewer scripting (`enableScripting`, default true) without a CSP | 6.2.108  |

`pdfjs-dist` 6.3.289 was the current npm release on the decision date [6]. No Go
module in the repository reads PDF.

## Decision

1. Import reads the Save to PDF file of an English LinkedIn profile. It does not
   read the data download and calls no LinkedIn API.
2. The browser parses the file with pdf.js 6.3.289, legacy build, a runtime
   dependency of the web app at that exact version. The file is never uploaded;
   the only write is the existing `POST /resumes` with the reviewed document.
3. pdf.js's core runs in a dedicated module Web Worker, loaded only on the
   import page. The page uses only the core API (`getDocument` and
   `getTextContent`): no viewer, no scripting, no annotation or form layer, no
   rendering, and no font loading. It passes no URLs, so pdf.js has nothing to
   fetch, and turns off WebAssembly (`useWasm: false`) and XFA. Version 6.3.289
   has no `isEvalSupported` option and compiles nothing to JavaScript. Its
   legacy build calls the `Function` constructor only in core-js's global-object
   fallback, which runs only where `globalThis` is missing; the policy has no
   `'unsafe-eval'`, so it would fail there. A unit test scans the pinned files
   for `eval` and `Function` calls.
4. The app page Content Security Policy uses `worker-src 'self'` (**Owner
   approval**). A same-origin worker can run only scripts that
   `script-src 'self'` already lets the page run. Public resume HTML, the print
   route, and the harness keep `worker-src 'none'`.
5. The page checks bytes it has read before and during parsing: at most 4 MiB, a
   `%PDF-` header, no encryption, at most 20 pages, at most 100,000 extracted
   characters, and at most 15 seconds, after which it terminates the worker.
6. The page accepts only a file that has LinkedIn's page footer on every page
   and English section headings. A PDF with footers but no English headings gets
   the English-only message; any other PDF gets the "not a LinkedIn profile PDF"
   message. Neither creates anything.
7. Import creates a new resume only, after a review screen. It maps the contact
   details the PDF shows; email and phone start unselected in the review. It
   does not import into an existing resume.
8. The data download path is deferred, not rejected. It is the candidate for
   Vietnamese profiles in a later release and needs its own ADR.

## Rejected alternatives

- **Parse on the server in Go.** The file would reach aboutme with the member's
  email and phone. The service would need an upload route, byte limits, and a
  PDF or archive parser in the Go process that shares one small host with
  Chromium, and the privacy notice could no longer say the file stays on the
  device.
- **Run pdf.js on the main thread** (its "fake worker"), keeping
  `worker-src 'none'`. A hostile or huge file freezes the tab, and the page
  cannot stop parsing at the time limit. It is the fallback if the owner revokes
  the policy change.
- **Another parser** (for example `unpdf` or a WebAssembly build of a C
  library). Each adds a package, `unpdf` wraps pdf.js anyway, and WebAssembly
  would need `'wasm-unsafe-eval'`.
- **Read the data download first.** It covers Vietnamese profiles, but takes a
  request, an email, and a ZIP reader.
- **A LinkedIn or Member Data Portability API.** Not open to a member in Vietnam
  or to this service.
- **Import into an existing resume.** Needs merge rules for every section; a new
  resume the person can edit is simpler and loses nothing.

## Consequences

- The privacy notice can say the file never reaches aboutme.
- A hostile file can only fail the import in the person's own tab; the server
  validates and sanitizes the created resume as it does any other.
- Only English profiles import. The page says so before the file picker and
  names the fix: set LinkedIn's language to English and save the PDF again.
- The layout is LinkedIn's, not a documented format. The parser keys on
  headings, line positions, and font sizes; a layout change fails the check in
  decision 6 or produces a review the person corrects, never a partial write.
- Save to PDF lists only three top skills and short sidebar lists, so the import
  is a starting point the person completes in the editor.
- A member can import someone else's profile. The page asks people to import
  their own; aboutme cannot check it.
- The import page loads about 450 KB of pdf.js (Brotli: 126 KB API and 322 KB
  worker, measured on 6.3.289). No other page loads it.
- The legacy build supports Chrome 125+, Firefox ESR, and Safari 18+ [7]; the
  page also needs `URL.parse` (Chrome 126+). Older browsers get a message
  instead of the file picker.
- `pdfjs-dist` upgrades follow the security advisories above and need review,
  like any runtime dependency.

## History

- Former ADR 0059 (2026-09-26, proposed, never accepted): read the data download
  in the browser, with no upload and a new resume only. Its browser-only parsing
  and new-resume rules carried into 0064; its data download source, its
  rejection of PDF import, and its "no contact data" rule did not.
- Former ADR 0064 (2026-09-27): read the English Save to PDF with pdf.js in a
  worker; app pages allow same-origin workers. Decision numbers here match it.

## Sources

Retrieved 2026-09-27.

1. [LinkedIn Help: Save a profile as a PDF](https://www.linkedin.com/help/linkedin/answer/a541960)
2. [Mozilla bug 1452075, CVE-2018-5158](https://bugzilla.mozilla.org/show_bug.cgi?id=1452075)
3. [NVD: CVE-2024-4367](https://nvd.nist.gov/vuln/detail/CVE-2024-4367)
4. [GHSA-hq66-cqwq-w95j, CVE-2026-16633](https://github.com/mozilla/pdf.js/security/advisories/GHSA-hq66-cqwq-w95j)
5. [GitHub Advisory Database: CVE-2026-16633](https://github.com/advisories/GHSA-hq66-cqwq-w95j)
6. [npm registry: pdfjs-dist](https://registry.npmjs.org/pdfjs-dist)
7. [pdf.js FAQ: browser support](https://github.com/mozilla/pdf.js/wiki/Frequently-Asked-Questions)
