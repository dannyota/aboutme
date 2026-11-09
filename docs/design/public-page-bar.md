# Public page bar

This page specifies the page bar of the
[public page bar and color scheme](public-page-theme.md) design, which owns the
owner decisions, the color scheme, and the display checks.

The bar is aboutme chrome, not part of the resume. It uses the application
tokens and the chrome typeface, and it stays quiet next to the resume: a pale
strip, muted credit text, and one small outlined button.

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
      <span>Download PDF</span>
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
- The download anchor's accessible name is its visible label.

## Copy

The bar follows the resume language, as today.

| Element  | Vietnamese resume     | Any other language      |
| -------- | --------------------- | ----------------------- |
| Credit   | `Tạo bằng aboutme.vn` | `Built with aboutme.vn` |
| Download | `Tải PDF`             | `Download PDF`          |

## Tokens

The bar takes the values from
[Typography and tokens](ui/typography-and-tokens.md). The public page does not
load the application theme, so the public page block of `ResumeDocument.vue`,
which builds into `print.css`, declares them as custom properties scoped to
`.public-toolbar`. The bar takes the dark column when the page's scheme is dark,
and under Match device when the viewer prefers dark.

| Role                  | Light                              | Dark                        | Source           |
| --------------------- | ---------------------------------- | --------------------------- | ---------------- |
| Bar ground            | `#F9F8F5`                          | `#0C1020`                   | Page background  |
| Bar bottom rule       | `#E5E1D6`                          | `rgba(230, 225, 210, 0.13)` | Border           |
| Mark                  | `#CC2649`                          | `#FF6B8A`                   | Seal text        |
| Credit text           | `#5C6178`                          | `#A5ABBF`                   | Muted foreground |
| Credit hover          | `#23399A`                          | `#A3B6F5`                   | Link             |
| Button fill           | `#FFFFFF`                          | `#141A2E`                   | Card             |
| Button border         | `#E5E1D6`                          | `rgba(230, 225, 210, 0.13)` | Border           |
| Button label and icon | `#23399A`                          | `#A3B6F5`                   | Link             |
| Button hover fill     | `#ECEEF6`                          | `#1A2138`                   | Secondary        |
| Focus ring            | `#26409C`                          | `#8FA6F0`                   | Ring             |
| Button shadow         | `0 1px 2px rgba(16, 27, 63, 0.06)` | none                        | Shadow xs        |

Measured contrast: the mark 5.0:1 light and 6.9:1 dark; credit text 5.7:1 and
8.3:1; the button label 9.9:1 on white, 8.5:1 on the hover fill, 8.7:1 dark, and
7.1:1 on the dark hover fill.

## Size and placement

| Property               | Value                                                   |
| ---------------------- | ------------------------------------------------------- |
| Bar height             | 48 px, or 56 px on a touch screen; 8 px block padding   |
| Inner row width        | the resume measure plus both page margins, centered     |
| Inner inline padding   | the resume's inline padding, `--screen-margin-x`        |
| Row layout             | brand left, download right; 16 px gap, 12 px on phones  |
| Wrap                   | the row wraps with an 8 px row gap; download goes last  |
| Credit type            | Be Vietnam Pro 500, 13 px on 20 px                      |
| Mark                   | 20 px high, 19 px wide, 8 px before the credit text     |
| Button                 | 32 px high, 10 px radius, 1 px border                   |
| Button padding         | 10 px left, 12 px right                                 |
| Button type            | Be Vietnam Pro 500, 14 px on 20 px; icon 16 px, gap 6px |
| Button, coarse pointer | 40 px high, same padding                                |
| Credit hover           | link color and underline with a 3 px offset             |
| Focus                  | 2 px ring, 2 px offset, on the credit and the button    |

The button matches the application `outline` button at size `sm`. It replaces
today's filled button, 2.75em high (about 35 px) in the template's link color.

The padding is `clamp(16px, (page width - measure) / 2, var(--page-margin-x))`,
the same value as the resume's own inline padding (see
[web.md](web.md#pagination-and-print)): 16 px on a phone, growing on a tablet,
the template margin once the full measure fits. So the brand and the text share
one left edge at every width.

At 1440 px the brand sits on the resume's left text edge and the button's right
edge on the resume's right text edge. On a phone the bar keeps a 16 px gutter.
Both languages fit one row at 360 px on a touch screen. At 320 px the English
row wraps and the button sits under the credit; the Vietnamese row still fits.

## Behavior

- The bar is not sticky. It scrolls away with the page, so it never covers the
  resume on any screen. A reader sees it first, where the download is most
  useful, and the page needs no script for it.
- The resume keeps its own top padding (`--page-margin-y`) under the bar.
- When the owner turned PDF download off, the bar holds the brand alone on the
  left, at the same height.
- Print hides the bar, as today.
- Under `forced-colors: active`, the mark strokes in `CanvasText` and the button
  shows a `ButtonBorder` border.
- On a sign-in resume the join invite sits at the bottom edge and the bar at the
  top, so they never overlap.
