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
  const heading = requiredClamp(customization.colors.primary, surface, 4.5);
  const body = requiredClamp(customization.colors.text, surface, 4.5);
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

export function useResumeStyles(tokens: ResumeStyleTokens): ResumeStyles {
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
    '--lh-heading': '1.2',
    '--header-align': tokens.header?.align ?? 'left',
    '--gap-section': `${tokens.spacing.sectionGap}px`,
    '--gap-entry': `${tokens.spacing.entryGap}px`,
    '--gap-heading': `${tokens.spacing.sectionGap * 0.4}px`,
    '--gap-block': `${tokens.spacing.entryGap * 0.4}px`,
    '--gap-inline': '0.5em',
    '--page-margin-x': `${page.marginXmm}mm`,
    '--page-margin-y': `${page.marginYmm}mm`,
    '--heading-transform':
      tokens.heading.style === 'titlecase'
        ? 'capitalize'
        : tokens.heading.style === 'uppercase'
          ? 'uppercase'
          : 'none',
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
    styles.header = colorRoles(tokens, tokens.colors.surface!);
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
