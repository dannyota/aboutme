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
`apps/web/test/fonts.test.ts`; and `apps/web/test/fixtures/font-coverage.txt`;
create `apps/web/app/assets/css/fonts.css`; and modify `apps/web/nuxt.config.ts`
only to register that stylesheet. Task 11's later `nuxt.config.ts` window starts
after this task and preserves the registration.

## Asset contract

The committed catalog manifest contains stable ID, display name, category, asset
paths and hashes, official source commit and paths, SPDX ID, copyright, license
hash, Reserved Font Names, asset policy, internal names, faces and axes,
measured coverage, deterministic fallback, and v1 down-conversion family.

For a subset, the manifest records `subset-original-name` only when the exact
license has no conflicting Reserved Font Name. Otherwise use an approved renamed
derivative. Source Sans 3 uses `unmodified-upstream` bytes so its reserved
“Source” name is retained legally. Never source an asset from a font aggregator
or a “personal use” download.

## Steps

- [ ] Write failing manifest-schema, path, hash, internal-name, license-file,
      and official-provenance tests. One missing condition rejects the entry.
- [ ] For each family in `docs/design/fonts.md`, pin exact upstream bytes. Use a
      pinned fonttools version only when the declared policy permits a subset
      under its final name. Record the complete command and source commit.
- [ ] Commit `font-coverage.txt` as the standalone English, Vietnamese, and
      renderer-punctuation codepoint fixture. Measure coverage from every final
      file and store exact codepoint sets or reproducible summaries for that
      fixture and any additional declared scripts. Do not trust upstream labels
      alone.
- [ ] Verify the exact OFL file permits the chosen policy without a fee. Retain
      notices. A fresh reviewer rechecks every Reserved Font Name and asset
      policy against the exact source before the catalog can land.
- [ ] Emit local-only `@font-face` rules. Do not claim a face that the manifest
      does not provide. Load only the selected face and required category
      fallback. `font-synthesis` is an element property, so Task 6 applies
      `font-synthesis: none` to the resume root and tests its computed value; it
      is not an `@font-face` descriptor.
- [ ] Implement `fontsReady`: explicitly request the selected face and fallback,
      then await both loads and `document.fonts.ready`. Unit-test the order and
      failure path with a fake `FontFaceSet`.
- [ ] Test that CSS has no remote URL, every asset hash and license matches,
      coverage labels equal the measured bytes, fallbacks exist, and the
      English/Vietnamese/renderer fixture set never reaches a platform font.
- [ ] Run the focused asset and license checker
      `(cd apps/web && npx vitest run test/fonts.test.ts)`, then
      `make web-lint web-typecheck web-test web-build`. Record total asset bytes
      and per-document requested bytes.

## Acceptance mapping

- AC-REN-003: local loading, truthful coverage, selected-face readiness, and
  deterministic fallback for the declared fixture set.
- AC-FONT-001: exact source, license, notices, fee-free rights, and Reserved
  Font Name policy for every asset.
