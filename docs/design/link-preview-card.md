# Link-preview card

This is the visual spec of the 1200 by 630 link-preview card and of its preview
in the publish dialog. [Link previews](link-previews.md#preview-card) sets the
card's inputs, text rules, and size limit, and
[ADR 0055](../adr/0055-stored-link-preview-card.md) records why the card is
built ahead of time and stored. All numbers below are CSS pixels on the 1200 by
630 canvas unless a row says otherwise.

## Canvas and boxes

The card page is server-rendered HTML with no script, under the print CSP. Every
size below is decided while the component renders, from the card envelope alone.

| Box            | x   | y         | Width | Height       | Notes                                               |
| -------------- | --- | --------- | ----- | ------------ | --------------------------------------------------- |
| Canvas         | 0   | 0         | 1200  | 630          | `#ffffff`, `overflow: hidden`, `position: relative` |
| Left band      | 0   | 0         | 285   | 630          | Decoration, `aria-hidden`                           |
| Right band     | 915 | 0         | 285   | 630          | Decoration, `aria-hidden`                           |
| Safe square    | 315 | 15        | 570   | 600          | No box of its own; the limit for identifying boxes  |
| Body box       | 335 | 39        | 530   | 500          | Photo, name, and headline                           |
| Footer box     | 335 | 559       | 530   | 32           | Mark and link                                       |
| Photo ring box | 496 | worst 39  | 208   | 208          | Centered in the body box column                     |
| Name box       | 335 | from flex | 530   | 1 or 2 lines | Width 100% of the body box                          |
| Headline box   | 335 | from flex | 530   | 1 or 2 lines | Width 100% of the body box                          |

The bands stop at x 285 and start at x 915, 30 px outside the safe square, so
the 630 by 630 center crop (x 285 to 915) is plain white with no tinted edge.
The body box and footer box keep a 20 px inset from the square's sides and a 24
px inset from its top and bottom.

## Type and color

The card sets `font-family: 'Be Vietnam Pro', sans-serif`,
`font-synthesis: none`, `hyphens: manual`, and `lang` from the envelope
language. Be Vietnam Pro has no CJK glyphs, so CJK text falls back to the render
image's fonts ([Fonts](fonts.md#coverage-and-fallback)). The template font,
colors other than the accent, and every Aurora token stay out. The name alone
sets `font-synthesis-weight: auto`, so a CJK fallback face without a 700 weight
still draws the name bold; Be Vietnam Pro has a real 700, so Latin and
Vietnamese names are unchanged.

| Text               | Weight | Size              | Line height    | Letter spacing | Color                                   |
| ------------------ | ------ | ----------------- | -------------- | -------------- | --------------------------------------- |
| Name               | 700    | 72, 64, 58, or 52 | 1.2            | -0.01em        | `--paper-ink` `#171A18`                 |
| Link in name place | 700    | 19 to 72          | 1.2            | -0.01em        | `aboutme.vn/` `#5F6763`, slug `#171A18` |
| Headline           | 400    | 32                | 1.35 (43.2 px) | 0              | `--paper-muted` `#5F6763`               |
| Footer text        | 500    | 14 to 24          | 32 px          | 0              | `--paper-muted` `#5F6763`               |

All text is centered (`text-align: center`). The name and headline use
`text-wrap: balance`, `white-space: normal`, `overflow-wrap: anywhere`, and
`word-break: normal`; the link lines use `white-space: nowrap`. Text never takes
the accent. `#5F6763` on white is 5.8:1.

## Accent decoration

The accent is the envelope's `#rrggbb`, already clamped to at least 3:1 against
white. It paints the photo ring at full strength and the bands and dots as
tints. Each tint is a mix with white in linear light, and its share is chosen so
the tint lands on a fixed luminance. A black accent and the lightest allowed
accent then give tints of equal lightness, gray for black and hued otherwise.

For a tint with target luminance `T`, where `Ya` is the accent's WCAG relative
luminance (0 for black, at most 0.30 after the clamp):

```text
share = (1 - T) / (1 - Ya), as a percentage rounded to 0.1
tint  = color-mix(in srgb-linear, <accent> <share>%, #ffffff)
```

| Tint             | Target `T` | Share, black | Share, `Ya` 0.30 | Black result |
| ---------------- | ---------- | ------------ | ---------------- | ------------ |
| Band, outer edge | 0.82       | 18.0%        | 25.7%            | `#EAEAEA`    |
| Band, inner edge | 0.985      | 1.5%         | 2.1%             | `#FDFDFD`    |
| Dot              | 0.60       | 40.0%        | 57.1%            | `#CBCBCB`    |

The component computes the three shares and passes them as custom properties.

**Bands.** The left band fills with
`linear-gradient(to right, <outer> 0, <inner> 285px)`. The right band mirrors
it: `<outer>` at x 1200, `<inner>` at x 915.

**Dots.** Each band carries a dot grid on a second layer over the band fill: 6
px circles (`radial-gradient(circle, <dot> 3px, transparent 3.5px)`), tile 24 by
24 px. Left band dot centers sit at x 12 + 24k (k 0 to 11, so 12 to 276) and y
15 + 24j (j 0 to 25, so 15 to 615): background position `0 3px`. Right band
centers sit at x 1188 - 24k (1188 to 924): background position `-3px 3px` in the
band's own box. The dot layer fades toward the square with
`mask-image: linear-gradient(to right, #000 0, #000 96px, transparent 285px)` on
the left and the mirror on the right. No dot edge passes x 279 or x 921.

## Photo

With a photo, the photo is the first item in the body column.

| Part       | Size                     | Style                                                                                                        |
| ---------- | ------------------------ | ------------------------------------------------------------------------------------------------------------ |
| Ring box   | 208 by 208, `flex: none` | `border: 4px solid <accent>`, `padding: 4px`, `#ffffff` fill, `border-radius: 50%`, `box-sizing: border-box` |
| Gap        | 4 px white               | The ring box padding                                                                                         |
| Image clip | 192 by 192               | `border-radius: 50%`, `overflow: hidden`, `position: relative`                                               |
| Image      | From the crop            | `position: absolute`, `object-fit: cover`, `alt=""`                                                          |

The crop is x, y, width, and height as fractions 0 to 1 of the image. The image
box uses the resume photo's math
(`apps/web/app/components/resume/primitives/Photo.vue`): left `-x / width`, top
`-y / height`, width `1 / width`, height `1 / height`, each as a percentage of
the clip. With no crop the crop is `0, 0, 1, 1`, and `object-fit: cover` centers
a non-square image. There are no initials.

## Fitting the text

The component fits text by estimating widths from the characters. The estimate
uses upper bounds of Be Vietnam Pro advances, measured from
`apps/web/app/assets/fonts/be-vietnam-pro-var.woff2` at weight 700, which also
bound weights 400 and 500. Because every estimate is at least the real width and
the estimate breaks lines in fewer places than Chromium does, the real text
never needs more lines than the estimate counts.

**Width table.** Split text into grapheme clusters (`Intl.Segmenter`,
granularity `grapheme`). A cluster's width is the class of its first code point
after NFD, so `ệ` counts as `e` and `Ơ` as `O`.

| Width (em) | Characters                                                               |
| ---------- | ------------------------------------------------------------------------ |
| 0.23       | Space U+0020                                                             |
| 0.35       | `` i j I . , : ; ' ’ ! ` ``                                              |
| 0.55       | `f l r t J ( ) [ ] { } " * / \ \| ~ < >`                                 |
| 0.67       | Other `a` to `z`, except `d m w`                                         |
| 0.80       | `d đ Đ`, `A` to `Z` except `I J M O Q W`, `0` to `9`, `- + = ? $ & ^ _`  |
| 0.90       | `m w M O Q # %`                                                          |
| 1.00       | CJK: Han, Hiragana, Katakana, Hangul, U+3000 to U+303F, U+FF01 to U+FF60 |
| 1.06       | `W @`                                                                    |
| 1.26       | Any other character, including other Latin letters, other scripts, emoji |
| 0.87       | The ellipsis `…` the cut appends                                         |

Letter spacing is left out of the estimate; the name's -0.01em only makes the
real text narrower.

**Line count.** `lines(text, size)` wraps greedily in a line of 530 px:

1. Break the text into units: runs between spaces, and each CJK cluster as a
   unit of its own with no space before or after it.
2. Start a line with the first unit. Add the next unit, plus 0.23em when a space
   separates them, while the line's width times `size` stays at or under 530.
   Otherwise start a new line.
3. A unit wider than 530 px on its own splits into cluster runs that each fit,
   as `overflow-wrap: anywhere` does.

**Name size.** Take the first size in 72, 64, 58, 52 for which
`lines(name, size) <= 2`. If none fits, use 52 and cut the name.

A line holds 7.36em at 72, 8.28em at 64, 9.14em at 58, and 10.19em at 52. For
example, `Nguyễn Thị Minh Khai` estimates at 11.94em and sets at 72 as
`Nguyễn Thị` over `Minh Khai`.

**Cut.** Lay the text out greedily at its size. Keep line 1. On line 2, add
units, or single clusters of a split unit, while the line plus 0.87em for the
ellipsis stays within 530 px. Drop trailing spaces and `, ; : – -`, then append
`…`. The cut text fits two lines by construction. The headline uses the same
count and cut at 32 px (16.56em a line).

The name and headline boxes also keep `display: -webkit-box`,
`-webkit-box-orient: vertical`, `-webkit-line-clamp: 2`, and `overflow: hidden`
as a backstop.

**Stacked marks.** The font's ascent is 1.0em and descent 0.265em. Stacked
Vietnamese capitals such as `Ẩ` reach 1.163em above the baseline, and `ỵ`
reaches 0.247em below it. At the name's 1.2 line height the first line's marks
rise 0.196em above the line box, and at the headline's 1.35 they rise 0.116em,
so `overflow: hidden` would clip them. The name box therefore has
`padding-top: 0.2em` and `padding-bottom: 0.05em`, and the headline box has
`padding-top: 0.15em` and `padding-bottom: 0.05em`. Equal negative margins
cancel the padding in layout, so the arithmetic below counts line boxes only.

**No name.** When the name is absent, the headline is absent too, and the name
place shows the link on two lines: `aboutme.vn/` (`#5F6763`), then the slug
(`#171A18`), each a block with `white-space: nowrap`. Its size is
`floor(530 / max(W(prefix), W(slug)))` clamped to 19 to 72, with `W` in em from
the width table. `aboutme.vn/` is 7.04em, so a slug of 7.04em or less gets 72
px; the widest 30-character slug (27.0em) gets 19 px. The footer then shows
`aboutme.vn` alone, so the address appears once.

**Footer text.** `aboutme.vn/<slug>`, or `aboutme.vn` when the name is absent,
sits on one line. Its size is `floor(489 / W(text))` clamped to 14 to 24; 489 px
is 530 minus the mark (29) and the gap (12). The slug `nguyen-thi-minh-khai`
(19.98em with the prefix) gets 24 px, and `aboutme.vn` alone (6.49em) gets 24
px. The text also sets `white-space: nowrap`, `overflow: hidden`,
`text-overflow: ellipsis`, and `max-width: 489px` as a backstop.

## Vertical layout

The body box is a flex column: `align-items: center`, `justify-content: center`.
Its items are the photo (when present), the name or link, and the headline (when
present). The name's top margin is 15 px after a photo and 0 without one; the
headline's top margin is 17 px. The column centers in the body box, so the photo
is always the top item and short text leaves even space above and below.

Worst case, a photo with two name lines at 72 px and two headline lines:

| Item                | Height           | y top | y bottom |
| ------------------- | ---------------- | ----- | -------- |
| Square top inset    | 24               | 15    | 39       |
| Photo ring box      | 208              | 39    | 247      |
| Gap                 | 15               | 247   | 262      |
| Name, 2 lines       | 2 x 86.4 = 172.8 | 262   | 434.8    |
| Gap                 | 17               | 434.8 | 451.8    |
| Headline, 2 lines   | 2 x 43.2 = 86.4  | 451.8 | 538.2    |
| Spare in body box   | 0.8              | 538.2 | 539      |
| Gap to footer       | 20               | 539   | 559      |
| Footer              | 32               | 559   | 591      |
| Square bottom inset | 24               | 591   | 615      |

The column is 208 + 15 + 172.8 + 17 + 86.4 = 499.2 px, within the 500 px body
box. The first name line's marks rise at most 0.2em x 72 = 14.4 px, to y 247.6,
0.6 px below the ring's lowest point at y 247. The 17 px gap keeps the name's
second-line descenders apart from the headline's stacked marks. The headline's
second-line descenders end 1.9 px inside its box.

Without a photo the worst column is 172.8 + 17 + 86.4 = 276.2 px, from y 150.9
to 427.1.

The footer box is a flex row: `justify-content: center`, `align-items: center`,
`gap: 12px`. It holds `AppLogo` with `markOnly` at `size="md"` (29 by 32) and
the footer text.

## Crops and small sizes

| View                    | Visible canvas | Result                                              |
| ----------------------- | -------------- | --------------------------------------------------- |
| Wide 1.91:1             | All            | Bands frame the white square                        |
| X 2:1                   | y 15 to 615    | Body box (y 39) and footer (to y 591) intact        |
| Center square           | x 285 to 915   | All white; text x 335 to 865 keeps 50 px margins    |
| Phone chat card, 300 px | Scale 0.25     | Name 13 to 18 px, headline 8 px, footer 3.5 to 6 px |

The headline and footer are secondary and may be small at 300 px; every app also
shows the preview title as text.

## Card markup

- The card uses no headings: the name, link, and headline are `p` elements, so
  the same component can sit inside the dialog.
- Bands and dots are two `div`s with `aria-hidden="true"`. The photo `img` has
  `alt=""`. The PNG's text alternative is `og:image:alt`
  ([Link previews](link-previews.md#text-rules), image text).
- The mark comes from `apps/web/app/components/app/AppLogo.vue`; its gradient is
  the brand's, not the accent.
- Nothing animates.

## Publish-dialog preview

The publish dialog shows the same card component, scaled down, inside a neutral
chat card with the preview title, description, and domain. The title and
description follow [Link previews](link-previews.md#text-rules) and track the
slug and page title fields as they change.

**Place and state.** The section sits between the browser tab fields and the
Options switches, in the dialog's 16 px field rhythm. It is collapsed each time
the dialog opens, and the card renders only once it is expanded. The dialog is
already long and scrolls, and the card with its photo is its heaviest part.

**Trigger.** A `Collapsible` whose trigger is a `Button` with `variant="ghost"`,
width `calc(100% + 1rem)`, height 36 px, `justify-content: space-between`,
`padding-inline: 8px`, and `margin-inline: -8px`, so its text lines up with the
field labels. It shows the heading in `text-sm font-medium` and a 16 px chevron
that points down when collapsed and up when open.

| Copy    | English                                                                    | Vietnamese                                                                                    |
| ------- | -------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- |
| Heading | Preview when your resume is shared                                         | Xem trước khi chia sẻ CV                                                                      |
| Caption | Chat apps and social networks show a card like this. Each app may crop it. | Ứng dụng chat và mạng xã hội hiển thị thẻ như thế này. Mỗi ứng dụng có thể cắt ảnh khác nhau. |

**Content.** 16 px under the trigger, a `figure` with `display: grid` and
`gap: 6px`: the chat card, then the caption as a `figcaption` in
`text-xs text-muted-foreground`.

| Part        | Rule                                                                                                                 |
| ----------- | -------------------------------------------------------------------------------------------------------------------- |
| Chat card   | Width 100%, `max-width: 360px`, left aligned; 1 px `border`, 10 px radius, `bg-muted`, `overflow: hidden`, no shadow |
| Image area  | `aspect-ratio: 1200 / 630`, `position: relative`, `overflow: hidden`, 1 px `border` at the bottom                    |
| Card        | The 1200 by 630 card at `position: absolute`, top left, `transform-origin: 0 0`, `transform: scale(k)`               |
| Text block  | `padding: 10px 12px`, grid with `gap: 2px`                                                                           |
| Title       | `text-sm` (14/20), `font-semibold`, foreground, at most 2 lines                                                      |
| Description | `text-sm` (14/20), `text-muted-foreground`, at most 2 lines                                                          |
| Domain      | `text-xs` (12/16), `text-muted-foreground`, the text `aboutme.vn`                                                    |

`k` is the image area's width divided by 1200, read with a `ResizeObserver`; the
card stays `visibility: hidden` until the first reading. The card stays white in
dark theme, like the resume sheet.

| Viewport         | Dialog body width | Chat card width | `k`    | Image height | Name at 72 |
| ---------------- | ----------------- | --------------- | ------ | ------------ | ---------- |
| 390 px phone     | 310               | 310             | 0.2583 | 162.8        | 18.6 px    |
| 640 px and wider | 560               | 360             | 0.3    | 189          | 21.6 px    |

**Accessibility.** The scaled card has `aria-hidden="true"`: it is decorative
here, and the title, description, and domain stay readable as text. The trigger
exposes `aria-expanded` and `aria-controls`, and the chevron is `aria-hidden`.
Reduced motion removes the chevron's 150 ms turn.
