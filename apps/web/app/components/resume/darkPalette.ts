import type { Customization } from '@aboutme/schema';

import {
  type OKLCH,
  oklchToHex,
  oklchToRGB,
  rgbToOKLCH,
} from './clampContrast';

type Colors = Customization['colors'];

// Tolerance for float noise at the sRGB cube's faces.
const GAMUT_EPSILON = 1e-6;
const CHROMA_STEP = 0.002;

const inGamut = (color: OKLCH): boolean => {
  const { r, g, b } = oklchToRGB(color);
  return [r, g, b].every(
    (channel) => channel >= -GAMUT_EPSILON && channel <= 1 + GAMUT_EPSILON,
  );
};

/**
 * `#rrggbb` for an OKLCH color. One outside sRGB loses chroma 0.002 at a time,
 * at the same lightness and hue, until it fits
 * (docs/design/public-page-theme.md, "Dark palette rule").
 */
export function oklchToGamutHex(color: OKLCH): string {
  let chroma = color.c;
  while (chroma > 0 && !inGamut({ ...color, c: chroma })) {
    chroma = Math.max(0, chroma - CHROMA_STEP);
  }
  return oklchToHex({ ...color, c: chroma });
}

const BACKGROUND_LIGHTNESS = 0.2;
const TEXT_LIGHTNESS = 0.9;
const SURFACE_LIGHTNESS = 0.27;
const DARK_SURFACE_FLOOR = 0.3;
const INK_CEILING = 0.94;

// A dark navy becomes a light steel blue; a mid tone keeps its lightness.
const liftInk = (color: string): string => {
  const source = rgbToOKLCH(color);
  return oklchToGamutHex({
    ...source,
    l: Math.min(Math.max(source.l, 1.1 - source.l), INK_CEILING),
  });
};

/** Whether the authored background is already dark (lightness below 0.5). */
export const isDarkBackground = (colors: Colors): boolean =>
  rgbToOKLCH(colors.background).l < 0.5;

/**
 * The five dark source colors for a resume's authored colors. The role
 * derivation then runs on them unchanged, once per surface. A background that
 * is already dark returns the colors as authored.
 */
export function darkColors(colors: Colors): Colors {
  if (isDarkBackground(colors)) return colors;
  const background = rgbToOKLCH(colors.background);

  const tone = background.c >= 0.01
    ? background
    : rgbToOKLCH(colors.primary);
  const text = rgbToOKLCH(colors.text);
  const dark: Colors = {
    background: oklchToGamutHex({
      l: BACKGROUND_LIGHTNESS,
      c: Math.min(0.25 * tone.c, 0.015),
      h: tone.h,
    }),
    text: oklchToGamutHex({
      l: TEXT_LIGHTNESS,
      c: Math.min(text.c, 0.015),
      h: text.h,
    }),
    primary: liftInk(colors.primary),
  };
  if (colors.accent !== undefined) dark.accent = liftInk(colors.accent);
  if (colors.surface !== undefined) {
    const surface = rgbToOKLCH(colors.surface);
    dark.surface = surface.l >= 0.5
      ? oklchToGamutHex({
          l: SURFACE_LIGHTNESS,
          c: Math.min(2 * surface.c, 0.035),
          h: surface.h,
        })
      : oklchToGamutHex({
          ...surface,
          l: Math.max(surface.l, DARK_SURFACE_FLOOR),
        });
  }
  return dark;
}
