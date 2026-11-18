import type { Customization } from '@aboutme/schema';

import {
  type OKLCH,
  oklchToHex,
  oklchToRGB,
  rgbToOKLCH,
} from './clampContrast';

type Colors = Customization['colors'];

const GAMUT_EPSILON = 1e-6;
const CHROMA_STEP = 0.002;

const inGamut = (color: OKLCH): boolean => {
  const { r, g, b } = oklchToRGB(color);
  return [r, g, b].every(
    (channel) => channel >= -GAMUT_EPSILON && channel <= 1 + GAMUT_EPSILON,
  );
};

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
const PAPER_SURFACE_STEP = 0.07;
const INK_CEILING = 0.94;

const liftInk = (color: string): string => {
  const source = rgbToOKLCH(color);
  return oklchToGamutHex({
    ...source,
    l: Math.min(Math.max(source.l, 1.1 - source.l), INK_CEILING),
  });
};

export const isDarkBackground = (colors: Colors): boolean =>
  rgbToOKLCH(colors.background).l < 0.5;

const surfaceLightness = (
  paper: string | undefined,
): { light: number; floor: number } => {
  if (paper === undefined) {
    return { light: SURFACE_LIGHTNESS, floor: DARK_SURFACE_FLOOR };
  }
  const lifted = rgbToOKLCH(paper).l + PAPER_SURFACE_STEP;
  return { light: lifted, floor: lifted };
};

export function darkColors(colors: Colors, paper?: string): Colors {
  if (isDarkBackground(colors)) return colors;
  const background = rgbToOKLCH(colors.background);
  const surfaceL = surfaceLightness(paper);

  const tone = background.c >= 0.01
    ? background
    : rgbToOKLCH(colors.primary);
  const text = rgbToOKLCH(colors.text);
  const dark: Colors = {
    background: paper ?? oklchToGamutHex({
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
          l: surfaceL.light,
          c: Math.min(2 * surface.c, 0.035),
          h: surface.h,
        })
      : oklchToGamutHex({
          ...surface,
          l: Math.max(surface.l, surfaceL.floor),
        });
  }
  return dark;
}
