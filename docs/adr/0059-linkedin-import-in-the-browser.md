# 0059: LinkedIn import reads the data export in the browser

Status: Proposed (2026-09-26).

The detailed design is [LinkedIn import](../design/linkedin-import.md).

## Context

People want to start a resume from their LinkedIn profile. LinkedIn's open API
permissions return only name, email, photo, and locale. Positions, education,
and skills need partner programs that a small resume service cannot join, and
the Member Data Portability API accepts only members in the EEA and Switzerland.
Most users of aboutme are in Vietnam.

Two files remain that a member can get alone. Profile "Save to PDF" supports
only English characters, by LinkedIn's own help page. The data download is a ZIP
of UTF-8 CSV files, one per profile section, ready within minutes when the
member picks categories. That download also holds data the resume does not need,
including other people's messages and connections.

## Decision

1. Import reads the LinkedIn data download (ZIP or its CSV files). There is no
   PDF import and no LinkedIn API call.
2. The browser parses the file. It is never uploaded, and the only write is the
   existing `POST /resumes` with the reviewed document.
3. The parser opens only eight allowlisted CSV entries and bounds file size,
   entry count, inflated bytes, rows, fields, and time. It uses the browser's
   `DecompressionStream`, adds no package, and needs no worker, so the Content
   Security Policy is unchanged.
4. Import creates a new resume only, after a review screen. It reads no contact
   data, birth date, address, or license numbers.

## Rejected alternatives

- **Parse the PDF.** English-only by LinkedIn's rule, a layout guess at best,
  and a PDF parser is a large attack surface.
- **Parse on the server in Go.** The standard library reads ZIP and CSV well,
  but the service would receive an archive that can hold messages and contacts,
  would need a new upload route, byte limits, and memory on a small host, and
  the privacy notice would have to describe that processing.
- **Use the Member Data Portability or a partner API.** Not open to a member in
  Vietnam or to this service.
- **Import into an existing resume.** Needs merge rules for every section; a new
  resume the person can edit is simpler and loses nothing.

## Consequences

- The privacy notice can say the file never reaches aboutme.
- LinkedIn does not document the export's CSV columns. The parser reads columns
  by header name, and a changed header skips that file with a notice until the
  mapping is updated.
- Browsers without `deflate-raw` decompression can import only unzipped CSV
  files.
- A hostile file can only fail the import in the person's own tab; the server
  validates and sanitizes the created resume as it does any other.
