---
version: 1
slug: "apps-web-app-pages-index-vue"
primary_target: "apps/web/app/pages/index.vue"
related_targets: ["apps/web/app/components/editor/EditorShell.vue","apps/web/app/components/editor/list/ResumeList.vue","apps/web/app/pages/login.vue","apps/web/app/components/editor/PublishDialog.vue"]
---

# Landing surface brief

Scope: `/` (Nuxt SSR, static, no data fetch). Visitor mode: Persuade. This brief
also seeds the world every application surface inherits (auth pages, resume
list, settings, editor shell, publish dialog); those surfaces are Operate and
keep familiar affordances, with the world living in precise details.

## Audience and job

A Vietnamese tech job seeker arriving from a recruiter's link, a GitHub README,
or a friend, deciding in seconds whether this is worth an account. They must
understand: it publishes a resume as a document at its own link, without a
profile, and they control discovery. Action: Create account (Sign in secondary).

## Proof and content

- The real renderer output for the Ada Lovelace `full` fixture (compiled in,
  rendered at SSR, no fetch; amend `docs/design/product.md` wording from
  "static server-rendered text" to "static server-rendered content").
- The three existing facts (yours to keep; one link per resume; bring your own
  agent) and the three publish choices (public resume, PDF download, SEO and
  GEO) in the product's exact vocabulary.
- No testimonials, counts, logos, or claims. License line stays.

## Direction contract

THESIS: The resume is a document and publishing is stamping it: a round red seal
at the sheet's foot, pressed by a person, never an agent. Refuses the template
carousel hero and any profile-card arrangement.

OWN-WORLD: White bond sheet on a cool grey desk (`#EDEFEB`); ink `#171A18`; pencil
grey `#5F6763` for secondary text and draft marks; hairline rules `#D8DDD9`; seal
red `#C8102E` only for the seal and the public state; signature blue-black
`#1F2A44` for the person's own actions (primary buttons, links, focus). Dark is a
lamp-lit desk at night (`#121614` desk, `#1A1F1C` panels); the sheet stays white.
Be Vietnam Pro throughout; uppercase only inside the seal. State is a mark, not
a hue: pencil ticks for saved, pencil text for draft, the seal for public. One 8
px module; the sheet stays whole at every width.

STORY: A visitor sees a finished resume with a seal naming its link and no
person anywhere, believes the document is the public thing, and creates an
account; later, Publish stamps.

FIRST VIEWPORT: 1440 wide, two columns on the desk. Left five twelfths:
headline "The resume is public. You are not.", one lead sentence, Create
account (signature ink) then Sign in (text). Right seven twelfths: the rendered
resume as a white A4 sheet at about 0.6 scale with a soft shadow, a red seal
about 96 px at its lower right, rotated a few degrees, ring text
"PUBLIC RESUME · ABOUTME.VN/ADA-LOVELACE". Below: three facts in one ruled
row, then the three publish choices as three ruled columns.

FORM: The stamped document (con dấu), position 1 on the grounded list, chosen
as the pick card; seed key aac522e4; code-led.

FINISH: unreviewed and undocumented is unfinished; this build ends with the
finish review, the verdict, DESIGN.md, and every shipping raster carrying its
provenance.

## Signature interaction and motion

Publish in the editor stamps: on success the seal lands on the preview sheet's
foot in one 180 ms press (scale 1.12 to 1, ink fading in); unpublish lifts it in
120 ms. Reduced motion: instant. Nothing else animates on load.

## Cross-surface reach

- Resume list: up to three sheets on the desk; each shows title, updated time,
  and a seal mark with its link or a pencil "Draft"; empty slots are faint
  outlines that create a resume; Rename and Delete in an overflow menu.
- Editor: four regions on the desk, sheet white in both themes; Publish is the
  only red control; Saved is a pencil tick; a small seal beside the title when
  public; fields commit on blur with human labels; outline uses section icons.
- Publish dialog: slug shown as `aboutme.vn/` plus input; three switches with
  their explanations; Publish in seal red; success shows the seal and Copy link.
- Auth pages: the form sits on the desk without a card; left-aligned title.
- Settings: sections divided by rules; devices as "Chrome on Linux", relative
  last-seen time, "This device"; connected agents and password blocks.

## Unresolved

- Exact seal artwork (SVG text-on-path) and whether the public page footer
  carries a small seal; the renderer itself does not change.
- Whether the preset used in the hero is `classic-serif` or `modern-sidebar`.
