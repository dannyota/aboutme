# 0013 — Contact details render in array order, as plain text, with `label`

Status: Accepted (2026-08-11)

## Context

`ResumeHeader`'s contact details carry three open questions, and all three are
golden-visible, so they must be settled before the first snapshot is committed.

**Order.** Design spec §5's renderer tree says "`ResumeHeader` (contacts per
`detailsOrder`)". That field does not exist. `resume.schema.json` deleted it and
records why on `personalDetail`: "Display order is the array order of
personalDetails.details (no separate detailsOrder field — order lives where it's
used, mirroring how customization.layout.sections orders content sections)."
`docs/specs/templates/contract.md` §5.1 already states the array-order rule and
says outright that it supersedes §5's `detailsOrder` reference, and phase 3's
`decisions.md` D12 says the same. Three documents agree; one line of the design
spec does not; and `contract.md` is marked `DRAFT v1 — not approved`, so as it
stood the plan was being asked to follow an unapproved supersession of the
design spec. This is exactly the shape ADR 0009 resolved for section order: the
authority already exists in the schema, and what is missing is the ratification.

**Linkification.** Genuinely contested. `contract.md` §5.1 permits `email` and
`phone` to render as `mailto:` / `tel:`; `decisions.md` D12 forbids it in v1.
The schema settles which reading is safe: it scopes an exact-lowercase
`https://` pattern to the four URL types (`website`, `linkedin`, `github`,
`twitter`) and says of the rest that "every other detail type (email, phone,
location, custom) has no design-spec-defined value format, so it is
intentionally left as the plain bounded string above — do not extend this
without a spec/ADR decision". Emitting a `mailto:` or `tel:` URL from a 256-char
unconstrained string is precisely such an extension, made in the renderer, where
the value has passed no format check at all.

**`label`.** `personalDetail.label` exists in the schema (optional,
maxLength 40) and `contract.md` §5.1 defines its behavior, but phase 3 never
mentions it. Left undecided, Task 6 ships without it and every committed golden
has to be regenerated when it arrives.

## Decision

**(a) Order.** Display order is the **array order** of
`personalDetails.details`. There is no separate order field and none will be
added. Design spec §5's `detailsOrder` reference is superseded for the ordering
claim only — the rest of that renderer-tree line stands. This ratifies
`contract.md` §5.1's supersession clause, which now carries this ADR's authority
rather than depending on the approval status of a draft. A detail with
`isHidden: true` is omitted entirely, before ordering is observable.

**(b) Rendering.** `email`, `phone`, `location`, and `custom` render as **plain
text** in v1. No `mailto:` links, no `tel:` links. Only the four https-validated
types render as an anchor, and the renderer **re-checks** the exact lowercase
`https://` prefix itself rather than trusting that the value was validated on
write — a value that fails the check renders as text, never as a link. Anchors
carry `rel="noopener noreferrer"`, unchanged from `contract.md` §5.1.

**(c) `label`.** `personalDetail.label` **ships in P3**. When present and
non-empty it replaces the type's default label; when absent or `""` the type's
default label is used. This is the absent-versus-cleared distinction the
document contract already guarantees everywhere else, and it costs one
conditional in `ResumeHeader`.

`contract.md` §5.1's "`email` and `phone` **may** render as `mailto:` / `tel:`"
is corrected by this ADR and edited in the same change; its `label` sentence and
its array-order sentence are ratified as written. Design spec §5's
`detailsOrder` reference is likewise corrected in place — the spec is `DRAFT v3`
and this is the design owner's ruling.

## Consequences

- The first goldens pin plain-text contacts and label substitution. Reversing
  (b) or (c) later regenerates every golden and every screenshot baseline, which
  is why they are ruled now rather than discovered at Task 6.
- Linkifying `email` or `phone` later is a schema change first: it needs a
  defined value format for those types, then a renderer change. The schema's "do
  not extend this without a spec/ADR decision" is satisfied by an ADR that adds
  the format, not by a renderer that assumes one.
- The renderer's own `https://` re-check is defense in depth against a document
  that reached storage before the pattern was tightened, or through any path
  that skipped validation. It is cheap and it must not be removed as redundant.
- `header.iconStyle` (`tokens.md` §3.4) is orthogonal to all three answers.
  Icons are presentation; they never add, remove, reorder, or reveal a detail,
  and they never suppress a detail's `label` or value.
- Nothing reads a detail order from anywhere but the array, so a future editor
  reorder is an array splice on `personalDetails.details` and needs no second
  authority to stay consistent — the same property ADR 0009 gives sections.
- `resume.schema.json`'s `personalDetailUrlTypes` description still cites
  "design spec §5: contacts per detailsOrder" as a cross-reference. It is
  descriptive text, not a constraint, and should be repaired to cite this ADR
  the next time that file is touched.
