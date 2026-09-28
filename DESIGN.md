# aboutme visual design

This is the living visual record of the implemented web application. The account
is private; the resume is the public document. The UI makes that distinction
visible through a colorful Aurora canvas, a calm white sheet of paper, and a
seal applied by the person.

## Product principle

The landing page leads with “CV của bạn. Chia sẻ theo cách của bạn.” by default
and “Your resume. Your link. Your control.” in English. It shows a compiled-in
resume rendered by the shared `ResumeDocument`, rather than a profile card or
template carousel. Publishing is a deliberate action with three named choices:
Public resume, PDF download, and SEO and GEO. The publish dialog also controls
the optional browser-tab title and emoji icon.

The editor, public page, and PDF use the same document renderer. Application
chrome may frame the renderer but does not change its output.

## Language coverage

Vietnamese is the default site language. The language choice persists in the
`aboutme-locale` cookie. Every application route renders in the chosen language:

- `/`, `/privacy`, `/terms`, `/verify`, and `/guide/mcp`.
- `/templates` and `/templates/{id}`.
- `/login`, `/login/second-factor`, `/register`, `/forgot-password`,
  `/reset-password`, and `/verify-email`.
- `/app/resumes`, `/app/new`, the editor at `/app/resumes/{id}`, and
  [LinkedIn import](docs/design/linkedin-import-ui.md) `/app/import/linkedin`.
- `/app/settings/sessions` and the agent-consent page `/authorize`.

The shell shows the language toggle on these routes. The editor top bar, the
editor's section sheet on phones, and the “Sign in to continue editing” dialog
carry their own toggle. A path outside this list renders in English.

Public resume chrome follows the resume language: Vietnamese resumes show “Tạo
bằng aboutme.vn” and “Tải PDF”; other languages show “Built with aboutme.vn” and
“Download PDF”. Resume content and renderer labels follow the resume's own
language, independent of the site language. The
[public page spec](docs/design/public-page-theme.md) sets the planned page bar
and the owner's color scheme.

## Visual direction

Colorful product UI. Calm white resume.
[ADR 0020](docs/adr/0020-application-visual-identity.md) records the aurora
canvas, the seal logo, the stamp, and the tokens.

- The application canvas is warm paper with large, soft radial glows: ink blue
  and indigo at the top corners, jade and ochre further down. The palette is
  keyed to the logo: navy ink for actions and seal red for public. The glows are
  CSS gradients on the body background under `data-ui="app"`, scroll with the
  page, and never animate.
- The resume is a whole white sheet with a neutral paper shadow. It stays white
  in dark theme and never takes an Aurora token, gradient, radius, or shadow.
- Blue leads: actions, links, focus, and `/verify`'s verified state. There are
  two ink blues: `--primary` fills and `--link` colors text. Indigo, jade, and
  ochre support templates, customization, and features. A section uses a few,
  never all.
- Red means public. Seal red marks the public state, the Publish action, the
  seal, and the logo's seal, which makes the same promise, and nothing else.
  Destructive actions use the burnt-orange destructive token as an outline, so
  Delete never reads as Publish.
- State is communicated by a mark or plain text: a pencil tick for saved, text
  for saving and draft, destructive text for failure, and a seal plus link for
  public.
- Chrome alignment follows an 8 px module. The sheet is never cropped.
- Copy is sentence case. Controls name the action they perform. English copy
  uses “resume”; Vietnamese localized copy uses “CV”.

## Parts

Each part holds whole sections of this spec.

| Part                                                                           | Sections                                                   |
| ------------------------------------------------------------------------------ | ---------------------------------------------------------- |
| [Logo and seal](docs/design/ui/identity-and-seal.md)                           | Logo; Seal and state marks                                 |
| [Typography and tokens](docs/design/ui/typography-and-tokens.md)               | Typography and tokens                                      |
| [Landing and Library](docs/design/ui/landing-and-library.md)                   | Landing; Library                                           |
| [Shell and editor](docs/design/ui/shell-and-editor.md)                         | Authenticated chrome and editor                            |
| [Responsive and accessibility](docs/design/ui/responsive-and-accessibility.md) | Responsive behavior; Interaction and motion; Accessibility |

## Tokens summary

Chrome uses `Be Vietnam Pro` with a 15 px body and 14 px controls, on the 8 px
module. Semantic tokens are defined on `:root` and switched by
`html[data-theme="dark"]`, an ink-black theme. `--primary` fills actions,
`--link` colors blue text, and `--seal` and `--seal-text` carry the public
state. Text meets WCAG AA over the brightest canvas glow. The resume sheet and
chrome that stands for it use the paper tokens, which never switch. The full
tables, spacing, radius, button, and dialog rules are in
[Typography and tokens](docs/design/ui/typography-and-tokens.md).

## Component guardrails

- Build chrome with Tailwind CSS v4 and shadcn-vue/reka-ui primitives.
- Keep primitives in `app/components/ui`, shared composites in
  `app/components/app`, and surface layout in pages or editor panels.
- Do not hand-style a generated primitive. A token-colored variant edit is the
  one exception, and only with a guard test; re-apply it after
  `apps/web/scripts/ui-add.sh` regenerates the primitive
  ([ADR 0019](docs/adr/0019-application-ui-toolkit.md)).
- Use the existing field, dialog, menu, sheet, button, and status components. Do
  not introduce raw controls or hand-written dialogs in a surface; the crop
  stage and ProseMirror content root are the custom-widget exceptions.
- Preserve visible labels, `aria-label` text, and stable `data-*` hooks when
  changing components. Tests query roles, labels, and those hooks.
- Keep the renderer pure and outside application chrome styling. Do not add
  page-specific values that bypass the semantic tokens.
