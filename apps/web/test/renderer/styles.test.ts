import type { Customization } from '@aboutme/schema';
import { describe, expect, it } from 'vitest';

import {
  effectiveSurfaceTarget,
  renderPageRule,
  useResumeStyles,
} from '../../app/components/resume/useResumeStyles';

const customization: Customization = {
  font: { family: 'inter', baseSizePx: 14 },
  colors: {
    primary: '#111111',
    text: '#222222',
    background: '#ffffff',
    accent: '#0066cc',
    surface: '#eeeeee',
  },
  spacing: { sectionGap: 16, entryGap: 8, lineHeight: 1.4 },
  heading: { style: 'uppercase', showRule: true },
  layout: {
    columns: 2,
    surfaceTarget: 'sidebar',
    sections: { main: [], sidebar: [] },
  },
  sectionDisplay: {
    skill: { style: 'bar' },
    language: { style: 'dots' },
  },
  pageFormat: 'a4',
  dateFormat: 'MM/YYYY',
};

describe('resume styles', () => {
  it('emits the unprefixed token vocabulary and scoped sidebar roles', () => {
    const styles = useResumeStyles(customization);
    expect(styles.root['--fs-base']).toBe('14px');
    expect(styles.root['--gap-section']).toBe('16px');
    expect(styles.root['--page-margin-x']).toBe('15mm');
    expect(styles.root['--heading-transform']).toBe('uppercase');
    expect(styles.root['--heading-letter-spacing']).toBe('0.06em');
    expect(styles.sidebar?.['--color-surface']).toBe('#eeeeee');
    expect(styles.header).toBeUndefined();
    expect(styles.page).toEqual({
      format: 'a4',
      widthPx: 794,
      heightPx: 1123,
      marginXmm: 15,
      marginYmm: 15,
    });
  });

  it('degrades absent and unavailable surface targets to none', () => {
    expect(
      effectiveSurfaceTarget({
        ...customization,
        colors: { ...customization.colors, surface: undefined },
      }),
    ).toBe('none');
    expect(
      effectiveSurfaceTarget({
        ...customization,
        layout: { ...customization.layout, columns: 1 },
      }),
    ).toBe('none');
  });

  it('renders exact A4 and Letter page rules with y then x margins', () => {
    expect(renderPageRule(useResumeStyles(customization).page)).toBe(
      '@page {\n  size: 210mm 297mm;\n  margin: 15mm 15mm;\n}',
    );
    const letter = useResumeStyles({
      ...customization,
      spacing: {
        ...customization.spacing,
        pageMargin: { x: 11, y: 7 },
      },
      pageFormat: 'letter',
    }).page;
    expect(renderPageRule(letter)).toBe(
      '@page {\n  size: 8.5in 11in;\n  margin: 7mm 11mm;\n}',
    );
  });
});
