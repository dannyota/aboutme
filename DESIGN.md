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

- `/`, `/privacy`, `/terms`, and `/verify`.
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

- The application canvas is a pale blue ground with large, soft radial glows:
  blue and indigo at the top corners, cyan and a faint pink further down. The
  glows are CSS gradients on the body background under `data-ui="app"`, scroll
  with the page, and never animate.
- The resume is a whole white sheet with a neutral paper shadow. It stays white
  in dark theme and never takes an Aurora token, gradient, radius, or shadow.
- Blue leads: actions, links, focus, and `/verify`'s verified state. There are
  two blues: `--primary` fills and `--link` colors text. Indigo, purple, and
  cyan support templates, customization, and features. A section uses a few,
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
`html[data-theme="dark"]`, a midnight-blue theme. `--primary` fills actions,
`--link` colors blue text, and `--seal` and `--seal-text` carry the public
state. Text meets WCAG AA over the brightest canvas glow. The resume sheet and
chrome that stands for it use the paper tokens, which never switch. The full
tables, spacing, radius, button, and dialog rules are in
[Typography and tokens](docs/design/ui/typography-and-tokens.md).

## Sign-in gate and join invite

[Sign in to view](docs/design/viewer-analytics/sign-in-to-view.md) lets an owner
require a viewer to sign in before a resume shows. This part specs the gate
page, the join invite, and the publish dialog's switch. The gate carries no
script and loads only the print-fonts stylesheet plus one inline `<style>`, so
it repeats literal token values rather than Tailwind classes, as the public page
bar already does for its own chrome
([public page bar](docs/design/public-page-theme.md#page-bar)). It stays light
in both the light and dark public page schemes.

### Gate page

A single centered card on the page background (`#F5F8FF`), never dark. Type is
Be Vietnam Pro, then Inter, `system-ui`, `sans-serif`, loaded by the print-fonts
stylesheet.

| Property        | Phone (375 px)                | Desktop (1280 px)          |
| --------------- | ----------------------------- | -------------------------- |
| Outer padding   | 16 px sides, 32 px top        | centered, min-height 100vh |
| Vertical align  | top-aligned                   | `align-items: center`      |
| Card width      | fills to viewport minus 32 px | 480 px max-width           |
| Card padding    | 24 px                         | 32 px                      |
| Card background | `#FFFFFF`                     | same                       |
| Card border     | 1 px `#DCE5F5`                | same                       |
| Card radius     | 14 px (dialog radius)         | same                       |

Card content, in document order:

1. `<h1>` the resume's public page title, 20 px/1.3, `#101B3F`.
2. One paragraph, 15 px/1.5, `#101B3F`, holding the
   [sign-in gate text](docs/design/viewer-analytics/legal.md#sign-in-gate-text)
   with its lead sentence in `<strong>` and the rest as plain text in the same
   paragraph, in the envelope language.
3. A column of provider links, one per offered provider in envelope order, 12 px
   gap, each full width: 44 px high, 1 px border `#DCE5F5`, 10 px radius,
   background `#FFFFFF`, text `#123EDB` 15 px/500, centered. Hover fills
   `#EAF2FF`; focus shows a 2 px `#1A5CEB` ring at 2 px offset. No provider logo
   or mark: the gate's closed rule set allows only text inside its anchors, so
   each link reads only "Tiếp tục với Google" / "Continue with Google" or "Tiếp
   tục với LinkedIn" / "Continue with LinkedIn".
4. The closed message, only when the envelope's `message` is not `none`, one
   paragraph 14 px, `#56648C`, 12 px top margin, no icon or color severity:
   - cancelled: "Bạn đã hủy đăng nhập. Chọn một nút ở trên để thử lại." / "You
     cancelled sign-in. Choose a button above to try again."
   - failed: "Chúng tôi không thể xác minh đăng nhập của bạn. Vui lòng thử lại."
     / "We could not verify your sign-in. Please try again."
5. The refusal line, 14 px, `#56648C`, 20 px top margin: "Không muốn đăng nhập?
   Hãy liên hệ trực tiếp chủ CV." / "Prefer not to sign in? Contact the resume
   owner directly."
6. The home link, 14 px, `#123EDB`, underlined, 16 px top margin, `href`
   `{origin}/`: "Về trang chủ aboutme.vn" / "Back to aboutme.vn".

Both languages fit this layout at 375 px with no horizontal scroll; the card
never exceeds the viewport width.

### Join invite

Appears only under the conditions in
[Join invite](docs/design/viewer-analytics/sign-in-to-view.md#join-invite). It
follows the public page's own color scheme
([public page bar tokens](docs/design/public-page-theme.md#tokens)): surface and
border take the bar's Button fill and Button border values, body text takes
Credit text, and the create-resume action takes the application `--primary` fill
(`#1A5CEB` light, `#72A0FF` dark; foreground `#FFFFFF` light, `#071126` dark),
never the seal token, which stays reserved for the public state.

**Card** (viewports at least 1024 px wide with at least 360 px of right margin
beside the resume): fixed position, 16 px from the bottom and right edges, 320
px wide, 14 px radius (dialog radius), 16 px padding, border 1 px (Button border
token), shadow the bar's Button shadow (light only; none dark). A close button
sits at the top right, 32 px square, an "×" glyph, `aria-label` "Đóng" /
"Close". Body text 14 px/1.5 below the close button's row, 12 px above the
button. The "Tạo CV miễn phí" / "Create a free resume" button spans the card
width, 40 px high, 10 px radius, `--primary` fill.

**Bar** (every other viewport): fixed to the bottom edge, full width, 56 px
content band plus `env(safe-area-inset-bottom)` padding below it; the page adds
bottom padding equal to that same total height. One row, 12 px inline padding, 8
px gap: body text (truncated with an ellipsis, hidden below 360 px), the same
button in a compact 36 px-high form, and the close button last. Background,
border, and text take the same tokens as the card.

**Shared behavior:**

- Entrance is a 200 ms fade and 8 px slide from its edge; `Escape` and the close
  button dismiss it. `prefers-reduced-motion` removes the transition entirely,
  matching the app's existing reduced-motion rule.
- The button opens `/register` or `/login` (the envelope's `data-join-invite`
  value) in the same tab, with no query string.
- The whole invite sits in a `role="region"` landmark labeled "Lời mời tạo CV
  miễn phí" / "Create a free resume invite".
- On a `sign_in` resume with the page bar, the bar stays at the top and the
  invite at the bottom, so they never overlap
  ([public page bar](docs/design/public-page-theme.md#page-bar)).

### Publish dialog: sign-in switch

The publish dialog's options `fieldset` gains a fourth `SwitchField`, directly
under the existing discovery switch, using the design's fixed label and line
([Setting](docs/design/viewer-analytics/sign-in-to-view.md#setting)). It follows
the same 12 px `gap-3` rhythm and `busy || !live` disable rule as the other
three switches, shown only when the capabilities read carries `signInToView`
true.

Turning it on disables the discovery switch above it: add `|| signInToView` to
its `:disabled` expression, and swap its `description` from the existing
discovery help text to a one-line note, restored when sign-in turns off again:

- "Tắt vì CV yêu cầu đăng nhập để xem." / "Off because this resume requires
  sign-in to view."

The discovery switch keeps showing its stored value, on or off, rather than
forcing its displayed position to off: the design keeps the stored
`seoGeoEnabled` value so the owner's choice returns when sign-in turns off, and
the disabled, dimmed switch with the note above makes plain that discovery is
off in effect regardless of the position shown.

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
