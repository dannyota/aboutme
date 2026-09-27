# Brand assets

The aboutme.vn identity is the seal ([ADR 0065](../adr/0065-seal-identity.md)):
a red stamp mark, an ink wordmark with a red dot, and the public stamp. The
product draws the logo and the stamp inline (`AppLogo`, `AppSeal`); the files
here are for everywhere else.

## Files and where they are used

| File                                        | Size        | Used by                                                               |
| ------------------------------------------- | ----------- | --------------------------------------------------------------------- |
| `banner-light.jpg`, `banner-dark.jpg`       | 1672 × 941  | The repository `README.md`, through a light and dark `<picture>`      |
| `share-image.jpg`                           | 1280 × 640  | GitHub social preview: upload in repository Settings → Social preview |
| `../../apps/web/public/og-image-v4.jpg`     | 1200 × 630  | Site Open Graph image in Vietnamese, the default (`i18n/meta.ts`)     |
| `../../apps/web/public/og-image-v4-en.jpg`  | 1200 × 630  | Site Open Graph image in English                                      |
| `aboutme-logo.svg`                          | vector      | The logo on light grounds                                             |
| `aboutme-logo-on-dark.svg`                  | vector      | The logo on dark grounds (seal `#FF6B8A`)                             |
| `aboutme-mark.svg`                          | vector      | The seal mark alone                                                   |
| `aboutme-stamp.svg`, `aboutme-stamp-vi.svg` | vector      | The public stamp for `aboutme.vn/danny`, English and Vietnamese       |
| `aboutme-wordmark.png`                      | 1360 × 256  | Raster logo for places that cannot take SVG                           |
| `aboutme-icon.png`                          | 1024 × 1024 | Raster app icon (store listings, profile pictures)                    |

The site icons live in `apps/web/public/` with versioned names: `favicon-v3.svg`
(dark-scheme aware), `icon-32-v3.png`, `apple-touch-icon-v3.png`,
`icon-192-v3.png`, `icon-512-v3.png`, `site-v3.webmanifest`, and `favicon.ico`.
The `-v2` icons and `og-image-v3.jpg` stay there, unlinked, for one release so
cached URLs keep working; delete them in the release after ADR 0065 ships.

## Rules

- Red appears once per logo: the mark and the dot. Never recolor the mark blue,
  never add a gradient, and never set the wordmark in a font.
- The stamp's word follows the language: PUBLIC, or CÔNG KHAI in Vietnamese.
- Sample content is Danny Pham at `aboutme.vn/danny` with `danny@example.com`.
  Employers and schools are fictional or generic (Lantern Pay, Kite Search,
  "University · Ho Chi Minh City"). Never put a real company or school in sample
  art.

## Changing the art

The banners, the social preview, the Open Graph images, the site icons, the app
icon, and the wordmark are HTML sources in `src/`, set in the vendored Be
Vietnam Pro with the product's token values. The SVGs here and `favicon-v3.svg`
are hand-kept copies of the `AppLogo` and `AppSeal` geometry. Each source names
its size and output in a `brand-render` meta tag. Edit a source, then render
from `apps/web`:

```sh
node scripts/render-brand.mjs                 # every source
node scripts/render-brand.mjs og-image-vi     # one source
```

The script uses the Playwright Chromium the e2e suite installs; set
`BRAND_CHROMIUM` to another Chromium binary if needed. When an Open Graph image
changes, bump its version in the file name and in `apps/web/app/i18n/meta.ts`,
because crawlers cache the old URL. The same goes for the icons.

`favicon.ico` is not rendered by the script. Pack it from 16, 32, and 48 px
renders of the mark without the inner ring, for example with ImageMagick:
`magick icon-16.png icon-32-v3.png icon-48.png favicon.ico`.

The stamp SVGs set their words in live text, so they need Be Vietnam Pro
installed to look right in design tools.
