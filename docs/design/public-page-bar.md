# Public page bar

This page specifies the page bar of the
[public page bar and color scheme](public-page-theme.md) design, which owns the
owner decisions, the color scheme, and the display checks.

The bar is aboutme chrome, not part of the resume. It uses the application
tokens and the chrome typeface, and it stays quiet next to the resume: a strip
on the page ground, muted credit text, and small outline pills. It keeps one row
on a phone.

## Structure

The bar is the first child of `.public-resume-page`, above `.public-measure`, so
it spans the full viewport width. An inner row aligns its content with the
resume text.

```html
<div class="public-toolbar">
  <div class="public-toolbar-inner">
    <span class="public-brand">
      <svg class="public-mark" aria-hidden="true" focusable="false">…</svg>
      <a class="public-credit" href="{canonical origin}/"
        >Built with aboutme.vn</a
      >
    </span>
    <a class="public-download" href="/api/v1/public/resumes/{slug}/pdf">
      <svg aria-hidden="true" focusable="false">…</svg>
      <span class="public-download-full">Download PDF</span>
      <span class="public-download-short" aria-hidden="true">PDF</span>
    </a>
  </div>
</div>
```

- The class names `public-toolbar`, `public-credit`, and `public-download` stay,
  so the server's HTML validator and the existing tests keep their hooks.
- The credit anchor keeps exactly `class` and `href` and one text child, as the
  validator requires. The mark therefore sits beside the anchor, not inside it.
  The validator accepts the download anchor's icon and label as they are, so Go
  needs no change.
- The mark is the logo's seal mark alone: the outer ring and the single-story
  `a`, tilted −8°, without the hairline inner ring, as at 24 px
  ([Logo](ui/identity-and-seal.md#logo)). It is plain SVG with no gradient, id,
  or style element. The public page loads no Tailwind, so `AppLogo` cannot
  render it; the mark geometry moves into a module that `AppLogo` and the public
  mark both import, so a logo change reaches both.
- The download icon is Lucide `download` at 16 px with `currentColor` stroke.
- The download anchor's accessible name is always the full label. Below 30 rem
  the full label is visually hidden and the short label `PDF` shows instead, so
  the visible text stays inside the name; from 30 rem the short label is not
  displayed.
- When the resume links at least one available project
  ([project entry points](project-entry-points.md)), `a.public-projects` sits
  left of the download anchor, 8 px apart, both inside `span.public-actions`, so
  they wrap together. Its accessible name is its visible label, and no count of
  projects appears.

## Copy

The bar follows the resume language, as today.

| Element  | Vietnamese resume     | Any other language      |
| -------- | --------------------- | ----------------------- |
| Credit   | `Tạo bằng aboutme.vn` | `Built with aboutme.vn` |
| Download | `Tải PDF`             | `Download PDF`          |
| Projects | `Dự án`               | `Projects`              |

## Tokens

The bar takes the values from
[Typography and tokens](ui/typography-and-tokens.md). The public page does not
load the application theme, so the public page block of `ResumeDocument.vue`,
which builds into `print.css`, declares them as custom properties scoped to
`.public-toolbar`. The bar takes the dark column when the page's scheme is dark,
and under Match device when the viewer prefers dark.

| Role                  | Light     | Dark      | Source           |
| --------------------- | --------- | --------- | ---------------- |
| Bar ground            | `#FFFFFF` | `#0E0A0D` | Page background  |
| Bar bottom rule       | `#E3E0F2` | `#2E242C` | Border           |
| Mark                  | `#DE1B5A` | `#E2788F` | Logo mark        |
| Credit text           | `#55517D` | `#A3959F` | Muted foreground |
| Credit hover          | `#4336C9` | `#B59BDB` | Link             |
| Button fill           | none      | none      |                  |
| Button border         | `#1A1650` | `#D3CAD0` | Foreground       |
| Button label and icon | `#1A1650` | `#D3CAD0` | Foreground       |
| Button hover fill     | `#F1EFFA` | `#251B23` | Accent           |
| Focus ring            | `#1A1650` | `#CDBCD9` | Ring             |
| Button shadow         | none      | none      |                  |

Measured contrast: the mark 4.76:1 light and 6.85:1 dark; credit text 7.34:1 and
6.89:1; the button label 16.48:1 light and 12.29:1 dark, 14.49:1 and 10.42:1 on
the hover fill; the button border is the label color.

## Size and placement

| Property               | Value                                                                                         |
| ---------------------- | --------------------------------------------------------------------------------------------- |
| Bar height             | 48 px, or 56 px on a touch screen; 8 px block padding                                         |
| Inner row width        | the resume measure plus both page margins, centered                                           |
| Inner inline padding   | the resume's inline padding, `--screen-margin-x`                                              |
| Row layout             | brand left, actions right; 16 px gap, 8 px on phones                                          |
| Wrap                   | one row down to 360 px; below that the row wraps with an 8 px row gap and the actions go last |
| Credit type            | Be Vietnam Pro 500, 13 px on 20 px; 12 px below 30 rem                                        |
| Mark                   | 20 px high, 19 px wide, 8 px before the credit text                                           |
| Button                 | 32 px high, pill radius, 1.5 px border                                                        |
| Button padding         | 10 px left, 12 px right; 8 px and 10 px below 30 rem                                          |
| Button type            | Be Vietnam Pro 500, 14 px on 20 px; icon 16 px, gap 6px                                       |
| Button, coarse pointer | 40 px high, same padding                                                                      |
| Projects button        | the button's style without an icon; 12 px each side, 10 px below 30 rem                       |
| Touch target           | 44 px high on a coarse pointer; buttons stay 40 px                                            |
| Credit hover           | link color and underline with a 3 px offset                                                   |
| Focus                  | 2 px ring, 2 px offset, on the credit and the button                                          |

The buttons match the application `outline` pill at size `sm`.

The padding is `clamp(16px, (page width - measure) / 2, var(--page-margin-x))`,
the same value as the resume's own inline padding (see
[web.md](web.md#pagination-and-print)): 16 px on a phone, growing on a tablet,
the template margin once the full measure fits. So the brand and the text share
one left edge at every width.

At 1440 px the brand sits on the resume's left text edge and the button's right
edge on the resume's right text edge. On a phone the bar keeps a 16 px gutter.
Both languages fit one row at 360 px on a touch screen with both buttons. At 320
px the row may wrap, and the actions sit under the credit.

## Behavior

- The bar is not sticky. It scrolls away with the page, so it never covers the
  resume on any screen. A reader sees it first, where the download is most
  useful, and the page needs no script for it.
- The resume keeps its own top padding (`--page-margin-y`) under the bar.
- When the owner turned PDF download off, the bar drops only Download PDF, at
  the same height. With no available project, the brand stands alone on the
  left.
- Print hides the bar, as today.
- Under `forced-colors: active`, the mark strokes in `CanvasText` and the
  buttons show a `ButtonBorder` border.
- On a sign-in resume the join invite sits at the bottom edge and the bar at the
  top, so they never overlap.
