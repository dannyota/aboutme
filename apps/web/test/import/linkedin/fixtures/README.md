# LinkedIn "Save to PDF" layout fixtures

These fixtures reproduce the structure, sizes, and baseline gaps recorded in
[docs/design/linkedin-import.md](../../../../../../docs/design/linkedin-import.md),
"Save to PDF structure" and "Tests", and
[ADR 0064](../../../../../../docs/adr/0064-linkedin-import-from-save-to-pdf.md).
Every name, employer, and contact detail is invented; no real profile's text
or file is used.

## What each fixture holds

- `basic-en.pdf`: 3 pages. A sidebar with a labeled phone, an unlabeled
  email, a profile URL and a website each labeled on the next line, three
  skills, two languages, two certificates. A bulleted summary. Four roles:
  two under one company's duration line, one continuing onto page 2. Two
  schools, one with its date wrapped onto a second line.
- `wraps-en.pdf`: 2 pages. A wrapped headline with a location, a wrapped
  profile URL, skill, language, certificate, and job title. The sidebar
  continues onto page 2.
- `dates-en.pdf`: every accepted date form, `Present`, a year-only range, an
  unreadable date, a start after the end, and `less than a year` as both a
  date duration and a group duration.
- `dropped-en.pdf`: Honors-Awards, Publications, Patents, Volunteer
  Experience, and Projects, the sections the parser drops.
- `vietnamese-letters.pdf`: an English profile whose name and one employer
  carry Vietnamese letters.
- `localized-vi.pdf`: the same layout with Vietnamese section headings, so
  the parser's English-only check rejects it. The page footers still read
  "Page N of M" in English, as a real localized export's do.
- `limits-en.pdf`: 20 pages, a 220-character headline, 80 roles, and one
  role description over 16 KiB of UTF-8.
- `injection-en.pdf`: `<script>alert(1)</script>`, `<img src=x
  onerror=alert(1)>`, and `javascript:alert(1)` in every mapped field and
  contact line, escaped in the XSL-FO source so the PDF's extracted text
  carries the literal characters.
- `other.pdf`: an ordinary two-page document with no "Page N of M" footer,
  for the not-a-LinkedIn-export message.

Every fixture uses one embedded font, Noto Sans Regular, subset per
document, matching the real export's single embedded font resource.

## How the PDFs are built

`generate-fo.mjs` (Node, run locally, not part of the container) emits the
`*.fo` sources from a small layout model: one absolutely positioned
`fo:block-container` per column per page, one `fo:block` per visual line,
with each line's vertical position or `space-before` computed from the
design's baseline gaps and the font's line-box metrics. A "wrapped" line is
therefore written as a second, ordinary line at the wrapped-line gap, not
produced by FOP's own line wrapping. Re-run it after editing a fixture's
content:

```bash
node apps/web/test/import/linkedin/fixtures/generate-fo.mjs
```

The `*.pdf` files are rendered once, by hand, from the committed `*.fo`
sources with Apache FOP 2.3 in a pinned Podman container, and are committed
alongside them. CI never regenerates them; `generate.sh` refuses to run when
`CI` is set.

Render (or re-render after editing a `*.fo` file) with at least 8 GiB of
free memory, from the repository root, under the shared local-check lock:

```bash
flock -o -w 900 "$(git rev-parse --path-format=absolute --git-common-dir)/aboutme-local-check.lock" \
  timeout 1200 systemd-run --user --scope --quiet \
  -p MemoryMax=2G -p MemorySwapMax=0 -p CPUQuota=200% \
  -- bash apps/web/test/import/linkedin/fixtures/generate.sh
```

`generate.sh` builds the image, then runs FOP once per `*.fo` file with the
network disabled, a 2 GiB memory cap, and this directory mounted read-only
as its source and read-write as its output.

Verify a rendered PDF's shape against the design's table with the existing
shape report, from the repository root, under the same lock (the report
needs `apps/web/node_modules`, symlinked only for the duration of this one
command):

```bash
flock -o -w 900 "$(git rev-parse --path-format=absolute --git-common-dir)/aboutme-local-check.lock" \
  timeout 120 systemd-run --user --scope --quiet \
  -p MemoryMax=2G -p MemorySwapMax=0 -p CPUQuota=200% \
  -- bash -c 'ln -s "$PWD/apps/web/node_modules" apps/web/node_modules && \
    node apps/web/scripts/linkedin-pdf-shape.mjs <path-to-pdf>; \
    rm -f apps/web/node_modules'
```

## Pinned versions and checksums

- Base image: `docker.io/library/eclipse-temurin@sha256:97137382c6f0c30427d9b7c44ad8b2d55ac823b0768c171d81a643ed219023c5`
  (`eclipse-temurin:17-jre-jammy` at the time this was pinned).
- Apache FOP 2.3:
  `https://archive.apache.org/dist/xmlgraphics/fop/binaries/fop-2.3-bin.tar.gz`,
  sha512
  `47858956b193d1b7e3045314eafc6d851750171ae322e601920b4ad4bde182fa8bb2cf17e3e6a592d6a8fc5a3b1e2561f000385ea5e41250745263ccd099a32e`.
  FOP 2.3's advanced typographic table reader cannot read this font's GDEF
  table (`UnsupportedOperationException: coverage set class table not yet
  supported`, a known FOP limitation with several current OpenType fonts),
  so `generate.sh` runs FOP with `-nocs` (disable complex script features);
  this fixture set uses no script that needs them.
- Noto Sans Regular, hinted static TTF, from the `NotoSans-v2.015` GitHub
  release:
  `https://github.com/notofonts/latin-greek-cyrillic/releases/download/NotoSans-v2.015/NotoSans-v2.015.zip`,
  sha256 of the release zip
  `0c34df072a3fa7efbb7cbf34950e1f971a4447cffe365d3a359e2d4089b958f5`, sha256
  of the extracted `NotoSans/hinted/ttf/NotoSans-Regular.ttf`
  `478c558ea716033cd60c03438f628dfa75694dcf6b5f6d505a2f05fd2b4f3823`. The
  font file itself is not committed; only the subset FOP embeds in each PDF
  is.
