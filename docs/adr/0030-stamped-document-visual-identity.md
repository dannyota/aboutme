# 0030 — Stamped-document visual identity for the application chrome

Status: Accepted (2026-09-04)

Amends [ADR 0029](0029-application-ui-toolkit.md), whose decision text keeps
"the existing zinc and emerald values", and the "Application UI" section of the
web and rendering design (Approved v4, section 5), whose chrome paragraph names
the zinc palette, Inter, and emerald. ADR 0029 lands on `main` with the phase PU
rebase; this record is accepted alongside it.

## Context

The application chrome, every page and editor panel outside the pure resume
renderer, carried the component toolkit's default identity: a zinc neutral
scale, Inter, and emerald for saved and active states. The landing page never
showed a resume. The resume list showed no publish state. Settings was unstyled.
Customization labels were schema paths. Nothing on any page was specific to a
resume builder for Vietnamese job seekers, and the product's one claim a
neighbor cannot copy, that the resume is public and the person is not, was
invisible.

The human owner reviewed the current pages on 2026-09-04, confirmed the primary
user (Vietnamese tech job seekers) and the positioning, and chose a visual
direction from a dealt hand of grounded and catalog candidates on the Impeccable
decision page (seed `aac522e4`).

## Decision

The chrome is the desk; the resume is the paper; publishing is stamping.

- **One red, one meaning.** Seal red (`#C8102E`) is used only by the seal
  component, the public state mark, and the Publish control. A round seal at the
  sheet's foot means "public at this link". Nothing else on any page is red
  except destructive actions, which use a distinct destructive token.
- **The person's own actions are signature ink.** Primary buttons, links, and
  focus use blue-black (`#1F2A44` light, `#D7DEEE` on ink in dark). Black
  buttons and green states are retired; `--positive` and `--chart-*` tokens are
  removed.
- **Desk and paper neutrals.** Page ground `#EDEFEB`, panels and inputs white,
  ink `#171A18`, pencil `#5F6763` for secondary text, hairline `#D8DDD9`. Dark
  is a lamp-lit desk (`#121614` ground, `#1A1F1C` panels); the rendered resume
  keeps its own document background in both themes.
- **State is a mark, not a hue.** Saved, saving, failed, draft, and public are a
  pencil tick, pencil text, destructive text, or the seal mark with the link,
  never colored chips.
- **One chrome typeface.** `'Be Vietnam Pro', 'Inter', system-ui, sans-serif`.
  Be Vietnam Pro is catalog rank 1, already bundled, and designed for Vietnamese
  diacritics. Uppercase appears only inside the seal.
- **One module, whole sheet.** An 8 px module rules chrome alignment. The
  preview sheet is never cropped; chrome restacks around it, down to 390 px.
- **Motion answers actions.** 150 ms color transitions, the primitives' own open
  and close, and the stamp: the public mark lands in one 180 ms press and lifts
  in 120 ms. Nothing animates on load. Reduced motion disables all of it.
- **The landing shows the product.** The home page renders one compiled-in
  sample resume through the shared renderer at server render, with its seal, and
  still performs no data fetch.

The renderer, its tokens, fonts, golden HTML, and screenshot baselines do not
change. The publish contract, the API, and the agent boundary do not change.

## Rejected alternatives

- **Keep the toolkit default.** Consistent and cheap, but indistinguishable from
  every shadcn product and blind to the positioning.
- **The sheet and the label** (the roll's assigned direction): the rendered
  resume with a green label tab carrying its link. Clear, but the
  sheet-in-a-hero is the category arrangement and the label is a timid accent.
- **The category standard**: headline, blue button, template carousel, four-step
  strip. Its testimonial slot has nothing true to hold.
- **Patent drawing sheet** and **boxed-lunch wrapper** catalog challengers: each
  explained the mechanism well but failed audience identification for this
  community; both stay adoptable on request.

## Consequences

- Phase PU's `theme.css` values are replaced; the shadcn semantic token names
  stay so generated primitives keep working. A `seal` button variant is added.
- Every application page loads Be Vietnam Pro; the variable font is already
  shipped for the renderer, so no new asset or license enters the catalog.
- Two shared components enter `components/app`: `AppSeal` and `StateMark` (which
  replaces `SaveStatus`).
- The landing HTML grows by one rendered resume; its base CSP is unchanged
  because the renderer already renders under it on public pages.
- The web design's product section changes "static server-rendered text" to
  "static server-rendered content".
- Browser proofs that assert hero button order, the password toggle text, list
  row buttons, the "Estimated pages" label, or customization label paths are
  updated with the change listed in the owning task.
- The Impeccable records (`PRODUCT.md`, the landing surface brief, and
  `DESIGN.md` written at finish from the built pages) become part of the
  repository so later UI work starts from the built world, not from memory.
