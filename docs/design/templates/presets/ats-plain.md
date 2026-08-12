# ATS Plain (`ats-plain`)

Status: **Draft v1** (2026-08-12). Not approved.

One column, black on white, no icons, no level widgets. For an applicant whose
resume is ingested by an applicant tracking system before a person sees it.
Every value serves one property: the characters a machine extracts from the PDF
are the characters the document stores, in the order it stores them.

Restraint for a human removes marks that compete for attention; restraint for a
machine removes marks that change the character stream or its order. So this
preset keeps a section rule (a human cue that costs a parser nothing) and spends
five lines on a stacked contact block (a parser cue that costs the human air).

## Defining decisions

OBSERVED = a mechanism can be pointed at; INFERRED = unverified received wisdom.

| Decision        | Value                             | Why                                                                                                                                                                                                                                                                                                                                                                                                   | Basis                                              |
| --------------- | --------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------- |
| Columns         | `1`, `placement: "keep"`          | Two columns are two independent grid flows (`print.md` §5); a position-based extractor orders lines by baseline and can merge across the gutter, so a sidebar line lands inside a main entry. One flow has exactly one serialisation. `keep` also means an apply never reorders the user's sections, where `byType` would.                                                                            | OBSERVED                                           |
| Heading case    | `normal`                          | `titlecase` maps to `text-transform: capitalize`, which uppercases the first letter of each word and leaves the rest, corrupting stored casing ("iOS" → "IOS"). `uppercase` bundles `0.06em` tracking (`tokens.md` §3.3), and extra inter-glyph advance moves an extractor toward inserting spurious spaces, never away. `normal` makes the heading's glyph run character-identical to `displayName`. | OBSERVED                                           |
| Section rule    | `showRule: true`                  | A border is a path in the PDF content stream and emits no characters, so it cannot reach the text layer. Once uppercase is refused it is the only section boundary besides `1.10 ×` base at weight 700.                                                                                                                                                                                               | OBSERVED                                           |
| Header details  | `stacked`, `left`                 | One line box per detail, so every position-based extractor breaks on the baseline change. Inline details are separated by a `0.5em` CSS gap (`geometry.md` §6), not a glyph, and gap-to-space thresholds are per-implementation. Left keeps one constant left edge for x-clustering.                                                                                                                  | OBSERVED (stacked), INFERRED (align)               |
| Header icons    | `iconStyle: "none"`               | An inline SVG contributes no characters, so at best it is neutral; on a rasterise-and-OCR path an envelope glyph can be read as a stray character beside the value.                                                                                                                                                                                                                                   | OBSERVED (neutrality), INFERRED (OCR)              |
| Skill, language | `text`, `text`                    | `bar` and `dots` put proficiency in geometry no text layer carries. `tag` chips are padded inline boxes whose gaps hit the same threshold problem as inline details. `text` renders the name only (`contract.md` §5.6): one entry, one line, plain text.                                                                                                                                              | OBSERVED                                           |
| Dates           | `Mon YYYY`                        | "Mar 2021" needs no field-order inference; "03/2021" is unambiguous only because 2021 > 12. `Mon` is a fixed English table in the renderer (`contract.md` §5.4), so the string never varies with `resumes.lng`.                                                                                                                                                                                       | OBSERVED (the string), INFERRED (parser behaviour) |
| Colour          | `#000000` on `#ffffff`, no accent | 21:1 is a fixed point of the contrast clamp (`colors.md` §5): what the preset specifies is exactly what renders, in preview, SSR and print. Maximum ink-to-paper separation is also the most robust input to OCR binarisation.                                                                                                                                                                        | OBSERVED (clamp), INFERRED (OCR)                   |
| Surface         | `surfaceTarget: "none"`           | A fill is parse-neutral in the text layer, but it survives to PDF only through `print-color-adjust: exact` plus `printBackground` (`print.md` §6), and it can only reduce the contrast of the text above it.                                                                                                                                                                                          | OBSERVED                                           |
| Page            | `a4`, margins `18 / 16` mm        | A4 gives 265 mm of flow against Letter's 247 mm — about 3.3 more body lines a page at 14 px / 1.45 — so fewer entries fragment across a page break, the one layout event that reliably separates an entry's title from its dates. Margins are parse-neutral; 18 mm is set for the reader and still leaves a 174 mm measure, about 90 characters.                                                      | OBSERVED (flow), INFERRED (crop safety)            |
| Type            | Source Sans 3, 14 px              | The v2 asset keeps exact upstream bytes because “Source” is a Reserved Font Name. The manifest records measured coverage and PDF embedding tests prove extraction. It is chosen for neutral letterforms, not because every catalog family has equal coverage. At 14 px the body is 10.5 pt.                                                                                                           | OBSERVED (asset policy), INFERRED (letterforms)    |
| Spacing         | `20 / 10 / 1.45`                  | The whitespace ladder is monotone: 4 px inside an entry (`--gap-block`), 8 px heading to first entry (`--gap-heading`), 10 px between entries, 20 px between sections, so a block segmenter sees a strict hierarchy. 1.45 keeps descenders clear of the next line's ascenders for OCR line segmentation.                                                                                              | OBSERVED (ladder), INFERRED (OCR)                  |

## Claims this preset does not make

| Folklore                                     | Status here                                                                                                                                                                                                                |
| -------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| "A non-standard font breaks ATS"             | Not assumed either way. P7 tests text extraction and embedding per family. This preset chooses legibility; a failed extraction test blocks that asset from an ATS claim, not from the user catalog.                        |
| "Photos break parsers"                       | An image XObject emits no characters. The reasons to drop a photo are screening convention and OCR block segmentation, not text extraction.                                                                                |
| "Rules, borders and colour break parsers"    | Vector paths and fills carry no characters at all.                                                                                                                                                                         |
| "Headings must be ALL CAPS to be recognised" | No mechanism found; case-insensitive dictionary matching is the baseline. The heuristic is INFERRED in both directions, so this preset takes the verifiable property (glyphs equal stored text) over the unverifiable one. |
| "Avoid tables and text boxes"                | Unreachable: the renderer emits neither.                                                                                                                                                                                   |

## Contrast, computed

WCAG 2.1 luminance, sRGB. Nothing needs clamping, so these render as specified.

| Role                                                             | Colour    | Ratio on `#ffffff` | Floor             |
| ---------------------------------------------------------------- | --------- | ------------------ | ----------------- |
| `--color-body`, 14 px                                            | `#000000` | 21.00:1            | 4.5:1             |
| `--color-heading`, 15.4 px bold                                  | `#000000` | 21.00:1            | 4.5:1             |
| `--fs-name`, 28 px bold                                          | `#000000` | 21.00:1            | 3:1 (large)       |
| `--color-meta`, 12.6 px (text mixed 25% to surface)              | `#404040` | 10.37:1            | 4.5:1             |
| `--color-link` = `--color-accent-text` (accent absent → primary) | `#000000` | 21.00:1            | 4.5:1             |
| `--color-rule` (accent mixed 60% to surface)                     | `#999999` | 2.85:1             | 1.5:1, decorative |

`--color-track` (`#cccccc`, 1.61:1) is never painted: with `text` widgets and no
fills, this preset renders no meaningful non-text element, so the 3:1 floor has
nothing to apply to. An all-black palette costs link affordance — the anchor and
its PDF annotation survive, and a user wanting blue sets `colors.accent`.

## Limits, and intent the token space could not carry

| Limit                                                                        | Consequence here                                                                                                                                                                                                                        |
| ---------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Photo cannot be suppressed ([limitations item 3](../limitations.md))         | `fixtures/full.json` carries one, so this preset's golden print shows a 96 px photo. Per the table above that is not an extraction hazard, but it is not this preset's intent either, and the only user workaround loses the crop.      |
| `iconKey` has no global off switch ([limitations item 5](../limitations.md)) | `header.iconStyle` covers header details only. All eight sections of `full.json` carry an `iconKey`, so eight lucide SVGs render. Heading text still extracts unchanged; the cost is x-alignment in layout analysis, plus the OCR path. |
| Range separator is U+2013 (§5.4), renderer-fixed                             | An ASCII hyphen or “to” may tokenize differently. The committed renderer punctuation fixture requires U+2013 in the selected face or its bundled fallback, so it does not silently reach a host font.                                   |
| No level is expressible as text (§5.6)                                       | `text` renders no widget, and the schema defines no 0–5 label vocabulary, so proficiency is unreadable to a parser in every style a parser can read. The one place the parse-safe choice loses information the document holds.          |
| `--gap-heading` is locked at `0.4 × sectionGap`                              | Heading-to-first-entry cannot be tuned independently of the section gap, so a heading cannot be bound harder to its own section.                                                                                                        |
| Apply resets `pageFormat` ([limitations item 2](../limitations.md))          | A US applicant who picks this preset silently ships A4.                                                                                                                                                                                 |
| No token reaches `/{slug}.md`                                                | The cleanest machine-readable output the product has is template-independent, so every claim here is about the PDF and the HTML page.                                                                                                   |

## Nearest siblings

| Sibling                       | Visible difference                                                                                                                                                                                                                          |
| ----------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| minimal-air                   | Restraint for the eye: it buys quiet with whitespace and soft greys. This preset spends whitespace on a five-line stacked contact block and refuses grey outright (21:1 body, `#404040` meta). Tells: rules on, contact column, pure black. |
| mono-print, government-formal | Both will reasonably reach for uppercase headings, and the formal one for a serif. This is the restrained preset with normal-case headings and no letter-spacing anywhere on the page, on the fidelity argument above.                      |
| high-contrast                 | Likely to land on the same `#000000` / `#ffffff`. The separators are base size (14 px, not a large-print size) and everything below the palette: stacked details, `text` widgets, `Mon YYYY`, one column by rule.                           |
