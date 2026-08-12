# Font catalog and licensing

Fonts are user choices. Script coverage, available faces, axes, and category are
catalog metadata, not admission gates. The only admission gate is the exact
license attached to the exact asset bytes.

## License gate

Every bundled asset must permit, without a purchase, subscription, usage fee, or
document fee:

- use in the hosted and self-hosted application;
- redistribution with the application;
- browser self-hosting;
- embedding in generated PDF and image output; and
- modification and subsetting when the declared asset policy modifies bytes.

The project loads no font CDN or metered font service. It ships required
copyright and license notices with the assets.

The current catalog uses the
[SIL Open Font License 1.1](https://openfontlicense.org/open-font-license-official-text/).
That license permits use, embedding, modification, and redistribution without a
fee, but it also carries conditions. Subsetting creates a modified font.
Reserved Font Names (RFNs) can therefore require a derivative name. The
[official OFL FAQ](https://openfontlicense.org/ofl-faq/) owns the interpretation
used by the asset review.

## Manifest contract

One committed manifest records each family:

- stable ID, display name, category, and catalog version;
- final asset paths and SHA-256 hashes;
- official upstream repository, commit, and source paths;
- SPDX license ID, copyright text, license-file hash, and RFNs;
- `unmodified-upstream`, `subset-original-name`, or `subset-renamed` policy;
- internal font names, weights, styles, and variable axes;
- measured codepoint coverage from the final vendored bytes;
- deterministic fallback for the guaranteed character set; and
- v1 down-conversion family.

Schema enum IDs and manifest IDs must match mechanically. A released catalog
entry and its asset hashes are immutable because changing metrics can reflow a
published resume. A font update uses a new catalog version or stable ID.

## Version 2 catalog

The initial v2 catalog admits the families below once the final byte and license
checks pass. Rankings guide preset defaults and UI order only. A lower rank is
still a valid choice.

Research is pinned to Google Fonts commit
[`038b637`](https://github.com/google/fonts/tree/038b637da7b3fd956a4ed93ffc607c3d5e4ce172).
Coverage labels use the repository's
[Vietnamese exemplar](https://github.com/google/fonts/blob/038b637da7b3fd956a4ed93ffc607c3d5e4ce172/lang/Lib/gflanguages/data/languages/vi_Latn.textproto)
but are recomputed from the final assets. The table links are a candidate index,
not final provenance. Final admission must resolve each family to its official
project repository, pin the exact bytes, and repeat the license and Reserved
Font Name check there.

| Rank | ID / family                                                                                                                                                             | Category and planned faces          | Asset policy          |
| ---: | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ----------------------------------- | --------------------- |
|    1 | [`be-vietnam-pro` — Be Vietnam Pro](https://github.com/google/fonts/tree/038b637da7b3fd956a4ed93ffc607c3d5e4ce172/ofl/bevietnampro)                                     | Sans, 100–900 roman/italic          | Subset, original name |
|    2 | [`inter` — Inter](https://github.com/google/fonts/tree/038b637da7b3fd956a4ed93ffc607c3d5e4ce172/ofl/inter)                                                              | Sans, 100–900 roman/italic          | Subset, original name |
|    3 | [`noto-sans` — Noto Sans](https://github.com/google/fonts/tree/038b637da7b3fd956a4ed93ffc607c3d5e4ce172/ofl/notosans)                                                   | Sans, 100–900 roman/italic          | Subset, original name |
|    4 | [`noto-serif` — Noto Serif](https://github.com/google/fonts/tree/038b637da7b3fd956a4ed93ffc607c3d5e4ce172/ofl/notoserif)                                                | Serif, 100–900 roman/italic         | Subset, original name |
|    5 | [`roboto` — Roboto](https://github.com/google/fonts/tree/038b637da7b3fd956a4ed93ffc607c3d5e4ce172/ofl/roboto)                                                           | Sans, 100–900 roman/italic          | Subset, original name |
|    6 | [`open-sans` — Open Sans](https://github.com/google/fonts/tree/038b637da7b3fd956a4ed93ffc607c3d5e4ce172/ofl/opensans)                                                   | Sans, 300–800 roman/italic          | Subset, original name |
|    7 | [`plus-jakarta-sans` — Plus Jakarta Sans](https://github.com/google/fonts/tree/038b637da7b3fd956a4ed93ffc607c3d5e4ce172/ofl/plusjakartasans)                            | Sans, 200–800 roman/italic          | Subset, original name |
|    8 | [`work-sans` — Work Sans](https://github.com/google/fonts/tree/038b637da7b3fd956a4ed93ffc607c3d5e4ce172/ofl/worksans)                                                   | Sans, 100–900 roman/italic          | Subset, original name |
|    9 | [`nunito-sans` — Nunito Sans](https://github.com/google/fonts/tree/038b637da7b3fd956a4ed93ffc607c3d5e4ce172/ofl/nunitosans)                                             | Sans, 200–1000 roman/italic         | Subset, original name |
|   10 | [`montserrat` — Montserrat](https://github.com/google/fonts/tree/038b637da7b3fd956a4ed93ffc607c3d5e4ce172/ofl/montserrat)                                               | Sans, 100–900 roman/italic          | Subset, original name |
|   11 | [`fira-sans` — Fira Sans](https://github.com/google/fonts/tree/038b637da7b3fd956a4ed93ffc607c3d5e4ce172/ofl/firasans)                                                   | Sans, 100–900 roman/italic          | Subset, original name |
|   12 | [`barlow` — Barlow](https://github.com/google/fonts/tree/038b637da7b3fd956a4ed93ffc607c3d5e4ce172/ofl/barlow)                                                           | Sans, 100–900 roman/italic          | Subset, original name |
|   13 | [`alegreya` — Alegreya](https://github.com/google/fonts/tree/038b637da7b3fd956a4ed93ffc607c3d5e4ce172/ofl/alegreya)                                                     | Serif, 400–900 roman/italic         | Subset, original name |
|   14 | [`spectral` — Spectral](https://github.com/google/fonts/tree/038b637da7b3fd956a4ed93ffc607c3d5e4ce172/ofl/spectral)                                                     | Serif, 200–800 roman/italic         | Subset, original name |
|   15 | [`literata` — Literata](https://github.com/google/fonts/tree/038b637da7b3fd956a4ed93ffc607c3d5e4ce172/ofl/literata)                                                     | Serif, 200–900 roman/italic         | Subset, original name |
|   16 | [`newsreader` — Newsreader](https://github.com/google/fonts/tree/038b637da7b3fd956a4ed93ffc607c3d5e4ce172/ofl/newsreader)                                               | Serif, 200–800 roman/italic         | Subset, original name |
|   17 | [`space-mono` — Space Mono](https://github.com/google/fonts/tree/038b637da7b3fd956a4ed93ffc607c3d5e4ce172/ofl/spacemono)                                                | Monospace, 400/700 roman/italic     | Subset, original name |
|   18 | [`crimson-pro` — Crimson Pro](https://github.com/google/fonts/tree/038b637da7b3fd956a4ed93ffc607c3d5e4ce172/ofl/crimsonpro)                                             | Serif, 200–900 roman/italic         | Subset, original name |
|   19 | [`eb-garamond` — EB Garamond](https://github.com/google/fonts/tree/038b637da7b3fd956a4ed93ffc607c3d5e4ce172/ofl/ebgaramond)                                             | Serif, 400–800 roman/italic         | Subset, original name |
|   20 | [`aleo` — Aleo](https://github.com/google/fonts/tree/038b637da7b3fd956a4ed93ffc607c3d5e4ce172/ofl/aleo)                                                                 | Slab serif, 100–900 roman/italic    | Subset, original name |
|   21 | [`cormorant-garamond` — Cormorant Garamond](https://github.com/google/fonts/tree/038b637da7b3fd956a4ed93ffc607c3d5e4ce172/ofl/cormorantgaramond)                        | Display serif, 300–700 roman/italic | Subset, original name |
|   22 | [`manrope` — Manrope](https://github.com/google/fonts/tree/038b637da7b3fd956a4ed93ffc607c3d5e4ce172/ofl/manrope)                                                        | Sans, 200–800 roman                 | Subset, original name |
|   23 | [`roboto-serif` — Roboto Serif](https://github.com/google/fonts/tree/038b637da7b3fd956a4ed93ffc607c3d5e4ce172/ofl/robotoserif)                                          | Serif, 100–900 roman/italic         | Subset, original name |
|   24 | [`roboto-mono` — Roboto Mono](https://github.com/google/fonts/tree/038b637da7b3fd956a4ed93ffc607c3d5e4ce172/ofl/robotomono)                                             | Monospace, 100–700 roman/italic     | Subset, original name |
|   25 | [`dm-sans` — DM Sans](https://github.com/google/fonts/tree/038b637da7b3fd956a4ed93ffc607c3d5e4ce172/ofl/dmsans)                                                         | Sans, 100–1000 roman/italic         | Subset, original name |
|   26 | [`atkinson-hyperlegible-next` — Atkinson Hyperlegible Next](https://github.com/google/fonts/tree/038b637da7b3fd956a4ed93ffc607c3d5e4ce172/ofl/atkinsonhyperlegiblenext) | Sans, 200–800 roman/italic          | Subset, original name |
|   27 | [`source-sans-3` — Source Sans 3](https://github.com/google/fonts/tree/038b637da7b3fd956a4ed93ffc607c3d5e4ce172/ofl/sourcesans3)                                        | Sans, existing v1 family            | Unmodified upstream   |

Source Sans 3 reserves “Source.” Its v2 entry therefore keeps exact upstream
bytes. A future subset must use a reviewed new internal and display name or
obtain written permission. Fonts from aggregators or “personal use” pages are
not valid provenance.

## Coverage and fallback

The UI reports coverage measured from final vendored bytes. It does not turn an
upstream language label into a guarantee. Families may lack Vietnamese marks,
italics, weights, or whole scripts and remain valid choices when labeled
truthfully. Chromium uses `font-synthesis: none`; an unavailable face falls back
instead of fabricating bold or italic metrics.

Noto Sans, Noto Serif, and Space Mono are the deterministic category fallbacks
for renderer-generated text plus the committed English, Vietnamese, and
punctuation fixture at `apps/web/test/fixtures/font-coverage.txt`. Content
outside that declared set remains accepted but may reach the pinned render
image's fonts or the viewer's platform fonts. The editor warns about that
boundary; it does not reject the selected family or the resume content.

Only the selected face and required fallback load for a document. Screenshot and
PDF capture waits for both. P7 renders one PDF per family and uses font
inspection to prove that the intended face is embedded.
