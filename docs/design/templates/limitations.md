# Known template limits

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
2. **Template apply resets `dateFormat`.** It is a regional preference, not
   visual design, but ADR 0008's wholesale replace covers it. `pageFormat` is
   the exception: paper follows where the owner prints, so a switch keeps it, as
   it keeps `font.textAlign` and `header.photoPosition`, and every preset ships
   on A4. _Cost of leaving it out:_ a switch can change how dates read. The
   editor warns before apply when the date format changes.
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
   ([Geometry](geometry.md)). Page margins are a token, `spacing.pageMargin`
   (0–40 mm per axis, default 15 mm), so a user needing one more line can narrow
   margins before touching `baseSizePx`.
7. **`baseSizePx` may be set to 10 (7.5 pt).** The schema's floor permits a
   document too small to read in print, and no publish-policy rule rejects it.
   The template cannot override it without breaking the user-owns-base-size
   boundary. _Cost of leaving it out:_ a user can publish an unreadable resume;
   the containment is an editor-side warning, not a contract change.
8. **Links are always underlined.** Color alone cannot mark a link: a monochrome
   print loses it, and WCAG AAA body contrast and G183's 3:1 link-versus-body
   contrast cannot both hold on a white page. The renderer therefore underlines
   every inline link, and no preset can unset it ([Geometry](geometry.md)).
