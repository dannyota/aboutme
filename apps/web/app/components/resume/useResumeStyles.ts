import type { Customization } from '@aboutme/schema';
import type { CSSProperties } from 'vue';

import { resolveFontSelection } from '../../utils/fontCatalog';
import {
  clampAgainst,
  contrastRatio,
  deriveLevelColors,
  mixInSRGB,
} from './clampContrast';
import { darkColors, isDarkBackground } from './darkPalette';
import {
  resolvePageGeometry,
  type ResolvedPageGeometry,
} from './pageMetrics';
import { DARK_PAPER, lightPaper } from './paper';

export type ResumeStyleTokens = Customization;

export interface ResumeStyles {
  root: Record<string, string>;
  header?: Record<string, string>;
  sidebar?: Record<string, string>;
  page: ResolvedPageGeometry;
}

export interface ResumeStyleOptions {
  readonly sheets?: boolean;
}

const SHEET_TYPE_FLOOR_PX = 16;
const SHEET_LINE_HEIGHT_FLOOR = 1.5;

type SurfaceTarget = NonNullable<Customization['layout']['surfaceTarget']>;

export function effectiveSurfaceTarget(
  customization: Customization,
): SurfaceTarget {
  const target = customization.layout.surfaceTarget ?? 'none';
  if (target === 'none' || customization.colors.surface === undefined) {
    return 'none';
  }
  if (target === 'sidebar' && customization.layout.columns === 1) return 'none';
  return target;
}

const requiredClamp = (
  color: string,
  surface: string,
  target: number,
): string => {
  const result = clampAgainst(color, [surface], target);
  if (result === null) {
    throw new Error('single-surface contrast is unsatisfiable');
  }
  return result;
};

type ColorScheme = 'light' | 'dark';

const colorRoles = (
  customization: Customization,
  surface: string,
  scheme: ColorScheme = 'light',
): Record<string, string> => {
  const accent = customization.colors.accent ?? customization.colors.primary;
  const darkSurface = scheme === 'light'
    && contrastRatio('#ffffff', surface) > contrastRatio('#000000', surface);
  const ink = (color: string, lightInk: string): string =>
    darkSurface && contrastRatio(color, surface) < 4.5
      ? lightInk
      : requiredClamp(color, surface, 4.5);
  const heading = ink(customization.colors.primary, '#ffffff');
  const body = ink(
    customization.colors.text,
    mixInSRGB('#ffffff', surface, 0.12),
  );
  const meta = requiredClamp(
    mixInSRGB(customization.colors.text, surface, 0.25),
    surface,
    4.5,
  );
  const accentText = requiredClamp(accent, surface, 4.5);
  const rule = requiredClamp(mixInSRGB(accent, surface, 0.6), surface, 1.5);
  const level = deriveLevelColors(accent, surface);
  const onAccent
    = contrastRatio('#000000', level.solid)
      >= contrastRatio('#ffffff', level.solid)
      ? '#000000'
      : '#ffffff';
  return {
    '--color-surface': surface,
    '--color-heading': heading,
    '--color-body': body,
    '--color-meta': meta,
    '--color-accent': accent,
    '--color-accent-text': accentText,
    '--color-accent-solid': level.solid,
    '--color-on-accent': onAccent,
    '--color-link': accentText,
    '--color-rule': rule,
    '--color-track': level.track,
  };
};

const scopedRoles = (
  roles: Record<string, string>,
  prefix: string,
): Record<string, string> =>
  Object.fromEntries(
    Object.entries(roles).map(([name, value]) => [
      name.replace('--color-', prefix),
      value,
    ]),
  );

const darkRoles = (roles: Record<string, string>): Record<string, string> =>
  scopedRoles(roles, '--dark-color-');

const paperRoles = (
  tokens: ResumeStyleTokens,
  target: SurfaceTarget,
): Record<string, string> => {
  const paper = lightPaper(tokens.colors);
  return {
    ...scopedRoles(colorRoles(tokens, paper), '--paper-color-'),
    '--paper-color-surface-header':
      target === 'header' ? tokens.colors.surface! : paper,
    '--paper-color-surface-sidebar':
      target === 'sidebar' ? tokens.colors.surface! : paper,
  };
};

const fontSizes = (base: number, prefix: string): Record<string, string> => ({
  [`${prefix}base`]: `${base}px`,
  [`${prefix}name`]: `${base * 2}px`,
  [`${prefix}headline`]: `${base * 1.15}px`,
  [`${prefix}heading`]: `${base * 1.1}px`,
  [`${prefix}title`]: `${base}px`,
  [`${prefix}subtitle`]: `${base}px`,
  [`${prefix}body`]: `${base}px`,
  [`${prefix}meta`]: `${Math.max(base * 0.9, 9)}px`,
});

const sheetTokens = (tokens: ResumeStyleTokens): Record<string, string> => ({
  ...fontSizes(
    Math.max(tokens.font.baseSizePx, SHEET_TYPE_FLOOR_PX),
    '--sheet-fs-',
  ),
  '--body-align-narrow': tokens.font.textAlign === 'justify'
    ? 'start'
    : tokens.font.textAlign ?? 'left',
  '--headline-margin-inline':
    (tokens.header?.align ?? 'left') === 'center' ? 'auto' : '0',
});

const headingTransform = (
  style: Customization['heading']['style'],
  lng: string,
): string => {
  if (style === 'uppercase') return 'uppercase';
  if (style !== 'titlecase') return 'none';
  return lng.toLowerCase().split('-')[0] === 'vi' ? 'none' : 'capitalize';
};

function darkScopes(
  tokens: ResumeStyleTokens,
  target: SurfaceTarget,
  paper?: string,
): { root: Record<string, string>; region?: Record<string, string> } {
  const colors = darkColors(tokens.colors, paper);
  const scheme: ColorScheme
    = isDarkBackground(tokens.colors) ? 'light' : 'dark';
  const dark = { ...tokens, colors };
  const pageSurface = colors.background;
  const region = target === 'none'
    ? undefined
    : darkRoles(colorRoles(dark, colors.surface!, scheme));
  return {
    root: {
      ...darkRoles(colorRoles(dark, pageSurface, scheme)),
      '--dark-color-surface-header':
        target === 'header' ? colors.surface! : pageSurface,
      '--dark-color-surface-sidebar':
        target === 'sidebar' ? colors.surface! : pageSurface,
    },
    ...(region === undefined ? {} : { region }),
  };
}

export function useResumeStyles(
  tokens: ResumeStyleTokens,
  lng = 'en',
  colorScheme?: 'dark' | 'system',
  options: ResumeStyleOptions = {},
): ResumeStyles {
  const sheets = options.sheets === true;
  const page = resolvePageGeometry(tokens);
  const target = effectiveSurfaceTarget(tokens);
  const pageSurface = tokens.colors.background;
  const dark = colorScheme === undefined
    ? undefined
    : darkScopes(tokens, target, sheets ? DARK_PAPER : undefined);
  const lineHeight = sheets
    ? Math.max(tokens.spacing.lineHeight, SHEET_LINE_HEIGHT_FLOOR)
    : tokens.spacing.lineHeight;
  const root = {
    ...colorRoles(tokens, pageSurface),
    '--color-surface-header':
      target === 'header' ? tokens.colors.surface! : pageSurface,
    '--color-surface-sidebar':
      target === 'sidebar' ? tokens.colors.surface! : pageSurface,
    ...dark?.root,
    ...(sheets ? paperRoles(tokens, target) : {}),
    '--font-family': resolveFontSelection(tokens.font.family).cssStack,
    ...fontSizes(tokens.font.baseSizePx, '--fs-'),
    '--lh-body': String(lineHeight),
    '--body-align': tokens.font.textAlign ?? 'left',
    '--body-hyphens': tokens.font.textAlign === 'justify' ? 'auto' : 'manual',
    '--lh-heading': '1.2',
    '--header-align': tokens.header?.align ?? 'left',
    '--gap-section': `${tokens.spacing.sectionGap}px`,
    '--gap-entry': `${tokens.spacing.entryGap}px`,
    '--gap-heading': `${tokens.spacing.sectionGap * 0.4}px`,
    '--gap-block': `${tokens.spacing.entryGap * 0.4}px`,
    '--gap-inline': '0.5em',
    '--gap-header': `${tokens.spacing.sectionGap * 1.5}px`,
    '--header-padding': '0',
    '--header-photo-gap': '0.9em',
    '--header-photo-gap-side': '1.25em',
    '--header-name-gap': '0.2em',
    '--header-details-gap': '0.5em',
    '--details-row-gap':
      `${Math.round(tokens.spacing.lineHeight * 150) / 1000}em`,
    '--details-column-gap': '1em',
    '--chip-icon-gap': '0.3em',
    '--page-margin-x': `${page.marginXmm}mm`,
    '--page-margin-y': `${page.marginYmm}mm`,
    '--heading-transform': headingTransform(tokens.heading.style, lng),
    '--heading-letter-spacing':
      tokens.heading.style === 'uppercase' ? '0.06em' : '0',
    '--rule-width': tokens.heading.showRule ? '1px' : '0',
    '--rule-gap': tokens.heading.showRule ? '0.25em' : '0',
    '--sidebar-ratio': '32%',
    '--column-gutter': '8mm',
    '--photo-size': '96px',
    '--photo-radius': '4px',
    '--bar-height': '4px',
    '--bar-radius': '2px',
    '--dot-size': '7px',
    '--dot-gap': '4px',
    '--tag-padding': '0.15em 0.5em',
    '--tag-radius': '3px',
    '--icon-size': '1em',
    ...(sheets ? sheetTokens(tokens) : {}),
  };
  const styles: ResumeStyles = { root, page };
  if (target === 'header') {
    styles.header = {
      ...colorRoles(tokens, tokens.colors.surface!),
      '--header-padding': '1em 1.25em',
      ...dark?.region,
    };
  }
  if (target === 'sidebar') {
    styles.sidebar = {
      ...colorRoles(tokens, tokens.colors.surface!),
      ...dark?.region,
    };
  }
  return styles;
}

export const articleStyle = (
  root: Record<string, string>,
): CSSProperties => ({
  ...root,
  fontSynthesis: 'none',
  printColorAdjust: 'exact',
  WebkitPrintColorAdjust: 'exact',
});

export function renderPageRule(page: ResumeStyles['page']): string {
  const size = page.format === 'a4' ? '210mm 297mm' : '8.5in 11in';
  const margin = `${page.marginYmm}mm ${page.marginXmm}mm`;
  return `@page {\n  size: ${size};\n  margin: ${margin};\n}`;
}
