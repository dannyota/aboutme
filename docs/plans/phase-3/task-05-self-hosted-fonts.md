# Task 5: Vendor the licensed font catalog

This task turns the reviewed [font catalog](../../design/fonts.md) into pinned,
self-hosted assets. Script coverage and available faces are metadata. Only the
license gate can exclude a family.

**Tier:** Normal implementation with a separate fresh license/provenance review.
It owns no schema file; Task 5B releases document v2 after these bytes are
final.

**Files:** `apps/web/app/assets/fonts/**`, including the manifest
`apps/web/app/assets/fonts/catalog.json` and regeneration guide
`apps/web/app/assets/fonts/README.md`; `apps/web/app/utils/fontsReady.ts`;
`apps/web/app/utils/fontCatalog.ts`; `apps/web/test/fonts.test.ts`; and
`apps/web/test/fixtures/font-coverage.txt`; create
`apps/web/app/assets/css/fonts.css` and `apps/web/public/font-licenses/**`; and
modify `apps/web/nuxt.config.ts` only to register that stylesheet. Task 11's
later `nuxt.config.ts` window starts after this task and preserves the
registration.

## Asset contract

The committed catalog manifest contains stable ID, display name, category, asset
paths and hashes, official source commit and input paths and hashes, archive URL
and hash when applicable, SPDX ID, copyright, license path and hash, Reserved
Font Names, asset policy, internal names, faces and axes, measured coverage,
deterministic fallback, and v1 down-conversion family.

The exact 26-row input matrix is frozen in `docs/design/fonts.md`. Its source
hashes identify the inputs to generation. They are not final subset hashes. Task
5 records both sets and rejects any source, archive, inner file, or license
whose SHA-256 differs from the design. It does not follow a branch, retarget a
tag, or substitute a newer release.

For a subset, the manifest records `subset-original-name` only when the exact
license has no conflicting Reserved Font Name. Otherwise use an approved renamed
derivative. Fira Sans and Source Sans 3 use `unmodified-upstream` bytes so their
reserved “Fira” and “Source” names are retained legally. Never source an asset
from a font aggregator, maintained fork, or “personal use” download.

The v2 payload is upright only. It supplies the renderer's 400 and 700 roles;
there is no italic style token. Variable inputs may retain a wider weight range,
but the manifest must describe the final bytes rather than infer faces or axes
from the upstream family.

The runtime mapping is one closed interface:

```ts
export interface ResolvedFontSelection {
  readonly id: string;
  readonly cssFamily: string; // exact @font-face family for this ID
  readonly fallbackId: "noto-sans" | "noto-serif" | "space-mono";
  readonly fallbackCssFamily: string;
  readonly cssStack: string; // quoted selected family, then bundled fallback
  readonly loadDescriptors: readonly [string, string]; // upright 400, then 700
}
export function resolveFontSelection(id: string): ResolvedFontSelection;
export function fontsReady(id: string, fonts?: FontFaceSet): Promise<void>;
```

`resolveFontSelection` rejects an unknown ID. `cssStack` contains no generic or
platform family. It de-duplicates the stack when the selected family is its own
category fallback. Each descriptor is the complete CSS font shorthand for the
resolved stack at `400 1em` or `700 1em`. `fontsReady` resolves the ID, calls
`fonts.load` for those descriptors in order, rejects an empty result, then
awaits `fonts.ready`. Renderer styles and every screenshot, print, and offline
probe consume this interface; no consumer maps an ID or category itself.

Every manifest entry names its exact runtime license file below
`/font-licenses/<id>/` and that file's hash. A generated
`/font-licenses/THIRD_PARTY_NOTICES.txt` lists each stable ID, display name,
copyright, SPDX ID, RFNs, and license path. Nuxt copies this tree into
`.output/public/font-licenses/`, so the distributed runtime artifact retains the
same notices as the source tree. The integration owner must add
`COPY apps/web/public/ ./apps/web/public/` to `deploy/web.Dockerfile`'s build
inputs before `npm --prefix apps/web run build`; the current file copies only
`apps/web/app/`. Its runtime stage already copies the complete `.output`, so no
separate image-only license copy is allowed to drift.

## Steps

- [ ] Record the reviewed font-catalog revision before fetching or generating an
      asset. Stop if it differs from this task's 26-row input matrix.
- [ ] Write failing manifest-schema, path, hash, internal-name, license-file,
      archive, and official-provenance tests. Assert the exact 26 design
      commits, input paths, input hashes, license paths, license hashes, RFNs,
      policies, and v1 fallbacks. One missing condition rejects the entry.
- [ ] Fetch only the exact immutable inputs in `docs/design/fonts.md`. Verify an
      archive hash before extraction and the selected inner-file hash after
      extraction. Verify direct repository blobs and license files against their
      listed hashes. A mismatch fails closed and requires reviewed design work;
      do not search for a replacement during implementation.
- [ ] Generate final WOFF2 assets with a pinned fonttools version only for
      `subset-original-name` rows. Record the complete command, tool version,
      input hash, and final hash. Copy Fira Sans 4.301 and Source Sans 3 3.052R
      without transformation and prove their final hashes equal their input
      hashes.
- [ ] Commit `font-coverage.txt` as the standalone English, Vietnamese, and
      renderer-punctuation codepoint fixture. Measure coverage from every final
      file and store exact codepoint sets or reproducible summaries for that
      fixture and any additional declared scripts. Do not trust upstream labels
      alone.
- [ ] Verify the exact OFL file permits the chosen policy without a fee. Copy
      each exact license into its manifest-named public runtime path and
      generate `THIRD_PARTY_NOTICES.txt`. A fresh reviewer rechecks every listed
      license hash, copyright notice, Reserved Font Name, and asset policy
      against the exact source before the catalog can land.
- [ ] Emit local-only `@font-face` rules. Do not claim a face that the manifest
      does not provide. Load only the selected face and required category
      fallback. `font-synthesis` is an element property, so Task 6 applies
      `font-synthesis: none` to the resume root and tests its computed value; it
      is not an `@font-face` descriptor.
- [ ] Implement `resolveFontSelection` and `fontsReady` exactly as above. Assert
      every ID maps to the manifest's CSS family and category fallback.
      Unit-test descriptor order, self-fallback de-duplication, unknown IDs,
      empty-load failure, and the final `ready` await with a fake `FontFaceSet`.
- [ ] Test that CSS has no remote URL, every asset hash and license matches,
      every design input and optional archive hash matches, coverage labels
      equal the measured final bytes, fallbacks exist, and the
      English/Vietnamese/renderer fixture set never reaches a platform font.
      Assert the manifest has exactly the approved 26 IDs, upright 400/700
      availability or truthful fallback metadata, and the declared v1 fallback
      for every entry. Build Nuxt and assert every manifest-named license plus
      `THIRD_PARTY_NOTICES.txt` exists byte-for-byte under
      `.output/public/font-licenses/`. Build the web image after the owner
      handoff and assert the same files exist in its runtime `.output/public`
      tree.
- [ ] Run the focused asset and license checker
      `(cd apps/web && npx vitest run test/fonts.test.ts)`, then
      `make web-lint web-typecheck web-test web-build`. Record total asset bytes
      and per-document requested bytes.

## Acceptance mapping

- AC-REN-003: local loading, truthful coverage, selected-face readiness, and
  deterministic fallback for the declared fixture set.
- AC-FONT-001: exact source, license, notices, fee-free rights, and Reserved
  Font Name policy for every asset.
