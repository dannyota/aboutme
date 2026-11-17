# Brand assets

The aboutme.vn identity is the seal
([ADR 0020](../adr/0020-application-visual-identity.md),
[logo and seal](../design/ui/identity-and-seal.md#logo)): a red stamp mark, an
ink wordmark with a red dot, and the public stamp. The product draws the logo
and the stamp inline (`AppLogo`, `AppSeal`); the files here are for everywhere
else.

## Files and where they are used

| File                                        | Size        | Used by                                                               |
| ------------------------------------------- | ----------- | --------------------------------------------------------------------- |
| `banner-light.jpg`, `banner-dark.jpg`       | 1672 × 941  | The repository `README.md`, through a light and dark `<picture>`      |
| `share-image.jpg`                           | 1280 × 640  | GitHub social preview: upload in repository Settings → Social preview |
| `../../apps/web/public/og-image-v8.jpg`     | 1200 × 630  | Site Open Graph image in Vietnamese, the default (`i18n/meta.ts`)     |
| `../../apps/web/public/og-image-v7-en.jpg`  | 1200 × 630  | Site Open Graph image in English                                      |
| `aboutme-logo.svg`                          | vector      | The logo on light grounds                                             |
| `aboutme-logo-on-dark.svg`                  | vector      | The logo on dark grounds (mark and dot `#E2788F`)                     |
| `aboutme-mark.svg`                          | vector      | The seal mark alone                                                   |
| `aboutme-stamp.svg`, `aboutme-stamp-vi.svg` | vector      | The public stamp for `aboutme.vn/danny`, English and Vietnamese       |
| `aboutme-wordmark.png`                      | 1360 × 256  | Raster logo for places that cannot take SVG                           |
| `aboutme-icon.png`                          | 1024 × 1024 | Raster app icon (store listings, profile pictures)                    |

The site icons live in `apps/web/public/` with versioned names: `favicon-v4.svg`
(dark-scheme aware), `icon-32-v4.png`, `apple-touch-icon-v4.png`,
`icon-192-v4.png`, `icon-512-v4.png`, `site-v4.webmanifest`, and `favicon.ico`.
The PNG icons are the red mark on an opaque white square. The previous set
(`favicon-v3.svg`, the `-v3` icons, `site-v3.webmanifest`, `og-image-v7.jpg`,
and `og-image-v6-en.jpg`) stays in `public/`, unlinked, for one release.

## Colors

| Part                             | Light ground                    | Dark ground                 |
| -------------------------------- | ------------------------------- | --------------------------- |
| Ground                           | `#FFFFFF`                       | `#0E0A0D`                   |
| Wordmark and headline            | `#1A1650`                       | `#D3CAD0`                   |
| Mark and the dot before `vn`     | `#DE1B5A`                       | `#E2788F`                   |
| Stamp strokes, stamp words       | `#DE1B5A`, `#C7134D`            | same, on its white ground   |
| Panel behind the sheet           | `#594BAD`                       | `#382961`                   |
| Coin fill, edge, label           | `#FFD23F`, `#E9B416`, `#1A1650` | same                        |
| Sheet ink, muted text, chip fill | `#171A18`, `#5F6763`, `#F0F2F1` | same; the sheet stays white |

## Rules

- Red appears once per logo: the mark and the dot. Never recolor the mark, never
  add a gradient, and never set the wordmark in a font.
- No blue anywhere in the art. Chàm appears only in the panel behind the sheet.
- The stamp's word follows the language: PUBLIC, or CÔNG KHAI in Vietnamese.
- Sample content is Danny Pham at `aboutme.vn/danny` with `danny@example.com`.
  Employers and schools are fictional or generic (Lantern Pay, Kite Search,
  "University · Ho Chi Minh City"). Never put a real company or school in sample
  art.

## Changing the art

The banners, the social preview, the Open Graph images, the site icons, the app
icon, and the wordmark are HTML sources in `src/` with the product's token
values. Headlines and the coin are set in the vendored Plus Jakarta Sans 800 at
line height 1.3; the sample sheet and the stamp are set in the vendored Be
Vietnam Pro. The Open Graph images, banners, and social preview share one
composition: the logo and the headline on the ground, and a panel holding a
white sheet tilted −3° with the stamp and a `0đ` coin. The SVGs here and
`favicon-v4.svg` are hand-kept copies of the `AppLogo` and `AppSeal` geometry.
Each source names its size and output in a `brand-render` meta tag; `opaque=1`
renders a PNG without transparency. Edit a source, then render from `apps/web`:

```sh
node scripts/render-brand.mjs                 # every source
node scripts/render-brand.mjs og-image-vi     # one source
```

The script uses the Playwright Chromium the e2e suite installs; set
`BRAND_CHROMIUM` to another Chromium binary if needed. When an Open Graph image
changes, bump its version in the file name and in `apps/web/app/i18n/meta.ts`,
because crawlers cache the old URL. The same goes for the icons.

`favicon.ico` is packed by hand, not by the script, from 16, 32, and 48 px PNGs
of the v4 icon (`icon-32-v4.png` and downscales of it and `icon-192-v4.png`),
stored as PNG entries; any ICO packer that keeps PNG entries works, for example
`magick icon-16.png icon-32-v4.png icon-48.png favicon.ico`.

The stamp SVGs set their words in live text, so they need Be Vietnam Pro
installed to look right in design tools.
