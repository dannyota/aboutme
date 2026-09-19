# 0045: PDF download name and metadata

Status: Accepted (2026-09-19)

Amends the PDF date rule in
[print determinism](../design/templates/print.md#7-determinism) and the public
download name in the [web design](../design/web.md).

## Context

A downloaded resume PDF is named after the public slug, or `resume.pdf` for the
owner's copy, so a recruiter's download folder fills with files that do not say
whose resume they are. The PDF's Title is the fixed word "Resume", and its
creation and modification dates are fixed to 1970-01-01 so that the same
document renders to the same bytes. Readers and file managers show that date as
the resume's age.

## Decision

1. **Download name.** Both the public PDF and the owner's PDF download as
   `<Full-Name>-Resume.pdf`:
   - The name splits into words on every character that is not a letter, mark,
     or number, and the words join with hyphens.
   - `filename` holds the ASCII form: diacritics are removed, `đ` and `Đ` become
     `d` and `D`, and only ASCII letters and digits remain.
   - `filename*` (RFC 5987) holds the same words in UTF-8, percent-encoded, so
     `Nguyễn Văn Đức` downloads as `Nguyễn-Văn-Đức-Resume.pdf` where the client
     supports it and as `Nguyen-Van-Duc-Resume.pdf` elsewhere.
   - Whole words are kept while the joined name fits 64 characters.
   - A name with no usable words downloads as `Resume.pdf` with no `filename*`.
2. **Title.** The print page title, which Chromium copies into the PDF Title, is
   `<full name> - Resume`. Control and format characters become spaces and white
   space collapses. A blank name leaves `Resume`.
3. **Dates.** CreationDate and ModDate carry the time the rendered revision was
   saved, in UTC. The same revision still renders the same bytes, so the public
   PDF cache and its strong ETag stay valid. A render with no revision time
   writes the Unix epoch.

## Consequences

- The name comes from the revision being served, never from cached headers, so a
  cached public PDF gets the current name.
- A title-only save changes the revision time, and with it the PDF dates.
- `docs/api/openapi.yaml` describes both Content-Disposition headers.
- The date replacement keeps the date length, so it moves no byte offset, and a
  time outside the years 1970 to 9999 fails the render.
