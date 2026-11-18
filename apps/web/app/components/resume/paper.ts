import type { Customization } from '@aboutme/schema';

type Colors = Customization['colors'];

export const LIGHT_PAPER = '#faf6ec';
export const DARK_PAPER = '#2a2723';

export const lightPaper = (colors: Colors): string =>
  colors.background.toLowerCase() === '#ffffff'
    ? LIGHT_PAPER
    : colors.background;
