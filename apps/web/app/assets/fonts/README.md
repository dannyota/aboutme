# Self-hosted font catalog

The files in this directory implement the frozen 26-family catalog in
`docs/design/fonts.md`. `catalog.json`, `fonts.css`, the WOFF2 files, the
runtime license tree, and the third-party notices are generated together. Do not
edit generated files by hand.

## Pinned environment

Regeneration requires Python 3.14.7 and the exact packages in
`tools/requirements.txt`:

```text
fonttools==4.63.0
brotli==1.2.0
```

Create a disposable environment outside the repository and run the generator
from the repository root:

```sh
python3 -m venv /tmp/aboutme-font-tools
/tmp/aboutme-font-tools/bin/pip install \
  -r apps/web/app/assets/fonts/tools/requirements.txt
/tmp/aboutme-font-tools/bin/python \
  apps/web/app/assets/fonts/tools/generate.py \
  --cache /tmp/aboutme-font-downloads
```

The cache may contain only bytes fetched by the generator. Every archive,
archive member, direct input, and license is checked against the SHA-256 in the
design before use. A mismatch excludes the family and makes verification fail.
The generator never chooses a replacement source.

The generator records the latest commit that changed `docs/design/fonts.md`.
Review that revision and the document's 26-row matrix before fetching or
generating assets. Run from a Git checkout so the revision is available.

## Outputs and verification

The generator writes:

- `*.woff2` and `catalog.json` in this directory;
- `../css/fonts.css`;
- `../../../public/font-licenses/**`.

It prints total vendored bytes and the selected-family plus fallback bytes for
each document choice. Verify all output with:

```sh
(cd apps/web && npx vitest run test/fonts.test.ts)
```

The checker independently parses final font tables with fontkit. It checks the
frozen provenance matrix, hashes, internal names, axes, faces, measured
coverage, deterministic fallback, license files, notices, local-only CSS, and
the runtime font-selection interface.
