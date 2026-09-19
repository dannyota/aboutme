import type { Customization } from '@aboutme/schema';

import { resolveFontSelection } from '../../utils/fontCatalog';
import {
  clampAgainst,
  contrastRatio,
  deriveLevelColors,
  mixInSRGB,
} from './clampContrast';
import {
  resolvePageGeometry,
  type ResolvedPageGeometry,
} from './pageMetrics';

export type ResumeStyleTokens = Customization;
export type { ResolvedPageGeometry } from './pageMetrics';

export interface ResumeStyles {
  root: Record<string, string>;
  header?: Record<string, string>;
  sidebar?: Record<string, string>;
  page: ResolvedPageGeometry;
}

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

const colorRoles = (
  customization: Customization,
  surface: string,
): Record<string, string> => {
  const accent = customization.colors.accent ?? customization.colors.primary;
  // On a dark surface a clamped ink turns a muddy mid-tone that passes the
  // ratio but reads faint, so an ink that fails outright goes near white.
  const dark
    = contrastRatio('#ffffff', surface) > contrastRatio('#000000', surface);
  const ink = (color: string, lightInk: string): string =>
    dark && contrastRatio(color, surface) < 4.5
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

// Title case is an English convention. Vietnamese capitalizes only the first
// word, so a Vietnamese resume keeps its headings as typed.
const headingTransform = (
  style: Customization['heading']['style'],
  lng: string,
): string => {
  if (style === 'uppercase') return 'uppercase';
  if (style !== 'titlecase') return 'none';
  return lng.toLowerCase().split('-')[0] === 'vi' ? 'none' : 'capitalize';
};

export function useResumeStyles(
  tokens: ResumeStyleTokens,
  lng = 'en',
): ResumeStyles {
  const page = resolvePageGeometry(tokens);
  const target = effectiveSurfaceTarget(tokens);
  const pageSurface = tokens.colors.background;
  const root = {
    ...colorRoles(tokens, pageSurface),
    '--color-surface-header':
      target === 'header' ? tokens.colors.surface! : pageSurface,
    '--color-surface-sidebar':
      target === 'sidebar' ? tokens.colors.surface! : pageSurface,
    '--font-family': resolveFontSelection(tokens.font.family).cssStack,
    '--fs-base': `${tokens.font.baseSizePx}px`,
    '--fs-name': `${tokens.font.baseSizePx * 2}px`,
    '--fs-headline': `${tokens.font.baseSizePx * 1.15}px`,
    '--fs-heading': `${tokens.font.baseSizePx * 1.1}px`,
    '--fs-title': `${tokens.font.baseSizePx}px`,
    '--fs-subtitle': `${tokens.font.baseSizePx}px`,
    '--fs-body': `${tokens.font.baseSizePx}px`,
    '--fs-meta': `${Math.max(tokens.font.baseSizePx * 0.9, 9)}px`,
    '--lh-body': String(tokens.spacing.lineHeight),
    // Justify hyphenates by the resume's lang (ADR 0041).
    '--body-align': tokens.font.textAlign ?? 'left',
    '--body-hyphens': tokens.font.textAlign === 'justify' ? 'auto' : 'manual',
    '--lh-heading': '1.2',
    '--header-align': tokens.header?.align ?? 'left',
    '--gap-section': `${tokens.spacing.sectionGap}px`,
    '--gap-entry': `${tokens.spacing.entryGap}px`,
    '--gap-heading': `${tokens.spacing.sectionGap * 0.4}px`,
    '--gap-block': `${tokens.spacing.entryGap * 0.4}px`,
    '--gap-inline': '0.5em',
    // Header gaps grow outward: icon to value, row to row, headline to
    // details, photo to name, header to body. A details row gap of 0.15 line
    // heights gives the rows the headline's line pitch.
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
  };
  const styles: ResumeStyles = { root, page };
  if (target === 'header') {
    styles.header = {
      ...colorRoles(tokens, tokens.colors.surface!),
      // A filled plate needs air between its edge and the name and details.
      '--header-padding': '1em 1.25em',
    };
  }
  if (target === 'sidebar') {
    styles.sidebar = colorRoles(tokens, tokens.colors.surface!);
  }
  return styles;
}

export function renderPageRule(page: ResumeStyles['page']): string {
  const size = page.format === 'a4' ? '210mm 297mm' : '8.5in 11in';
  const margin = `${page.marginYmm}mm ${page.marginXmm}mm`;
  return `@page {\n  size: ${size};\n  margin: ${margin};\n}`;
}
