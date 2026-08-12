# Known template limits

Status: **Draft v2** (2026-08-12). Not approved.

These limits are accepted for the current template contract. The editor must
warn where noted. Changing a stored field requires the document-version process
in [ADR 0017](../../adr/0017-resume-document-versioning.md).

## 9. Known contract limits

1. **No template identity in the document.** `customization` is
   `additionalProperties: false` and stores no `templateId`, so two templates
   can differ only by their values for the 25 leaf tokens. `colors.surface`,
   `layout.surfaceTarget`, and the `header` object make a tinted header band or
   sidebar and a distinct header treatment expressible. Deeper structural
   variety — a timeline rail, per-template markup — remains unreachable. ADR
   0008 fixed placement expressiveness; the residue of its concern ("templates
   would differ only by fonts and spacing") survives in much weaker form. _Cost
   of leaving it out:_ every preset must reuse the fixed renderer structure.
   Adding a timeline, a new region, or other structural template requires a
   later document release rather than a new JSON file.
2. **Template apply resets `pageFormat` and `dateFormat`.** Both are regional
   preferences, not visual design, but ADR 0008's wholesale replace covers them.
   _Cost of leaving it out:_ a user on A4 who tries a Letter preset ships a
   Letter PDF without noticing. The editor warns before apply when either value
   changes. Apply remains wholesale as ADR 0008 requires.
3. **No photo visibility control.** A photo lives in `personalDetails.photo`;
   nothing in `customization` can suppress it, and §3 has no `showPhoto` flag.
   An ATS-oriented or photo-free template must therefore still render a photo
   that is present — dropping it would silently unrender document content. _Cost
   of leaving it out:_ users hide a photo by deleting it, losing the crop.
4. **No section-level hidden flag.** Only entries have `isHidden`, so hiding a
   whole section means hiding every entry or deleting the section. _Cost:_ an
   editor bulk action masks it, at N writes and a lossy delete/undo path.
5. **`iconKey` has no global on/off.** An icon-free template would drop the
   user's chosen icon with no explanation, so in v1 every template renders
   icons. _Cost:_ no template can be icon-free.
6. **No sidebar width token.** The sidebar ratio is renderer-fixed
   ([Geometry](geometry.md)). The page-margin half of this item is resolved:
   `spacing.pageMargin` (0–40 mm per axis, default 15 mm) is now a token, so a
   user needing one more line can widen margins before touching `baseSizePx`.
   The fixed ratio is accepted for this release.
7. **`baseSizePx` may be set to 10 (7.5 pt).** The schema's floor permits a
   document too small to read in print, and no publish-policy rule rejects it.
   The template cannot override it without breaking the user-owns-base-size
   boundary. _Cost of leaving it out:_ a user can publish an unreadable resume;
   the containment is an editor-side warning, not a contract change.
8. **Link distinguishability is resolved.** The renderer owns `text-decoration`,
   and two independent preset designs proved that color alone cannot carry the
   distinction: on a monochrome print a link is indistinguishable from body ink,
   and a preset targeting WCAG AAA text contrast cannot simultaneously satisfy
   G183's 3:1 link-versus-body requirement — the two constraints are
   arithmetically incompatible on a white page. **Adopted: a renderer-wide
   underline on every inline link**, fixed in the codebase and unsettable by any
   preset ([Geometry](geometry.md)). It is the standard resolution G183 exists
   to avoid needing, it costs one CSS declaration, and it makes AAA-plus-G183
   reachable instead of arithmetically impossible. Every template's look changes
   slightly, which is why this was fixed before the first golden is approved.
