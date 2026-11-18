import type { Customization, Resume } from '@aboutme/schema';
import { TEMPLATES } from '@aboutme/schema/templates';
import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { describe, expect, it } from 'vitest';

import {
  contrastRatio,
  rgbToOKLCH,
} from '../../app/components/resume/clampContrast';
import {
  resolveRenderModel,
} from '../../app/components/resume/resolveRenderModel';
import { useResumeStyles } from '../../app/components/resume/useResumeStyles';

const base: Customization = {
  font: { family: 'inter', baseSizePx: 13 },
  colors: {
    primary: '#12293f',
    text: '#1a1e22',
    background: '#ffffff',
    accent: '#2563eb',
  },
  spacing: { sectionGap: 16, entryGap: 8, lineHeight: 1.3 },
  heading: { style: 'uppercase', showRule: true },
  header: { align: 'center', detailsLayout: 'inline', iconStyle: 'outline' },
  layout: { columns: 1, sections: { main: [], sidebar: [] } },
  sectionDisplay: { skill: { style: 'bar' }, language: { style: 'dots' } },
  pageFormat: 'a4',
  dateFormat: 'MM/YYYY',
};

const ROLES = [
  'surface',
  'heading',
  'body',
  'meta',
  'accent',
  'accent-text',
  'accent-solid',
  'on-accent',
  'link',
  'rule',
  'track',
] as const;

const SIZES = [
  'base',
  'name',
  'headline',
  'heading',
  'title',
  'subtitle',
  'body',
  'meta',
] as const;

const customize = (patch: (value: Customization) => void): Customization => {
  const value = structuredClone(base);
  patch(value);
  return value;
};

const sheets = (
  customization: Customization,
  colorScheme?: 'dark' | 'system',
) => useResumeStyles(customization, 'en', colorScheme, { sheets: true });

const sheetKeys = (scope: Record<string, string> | undefined): string[] =>
  Object.keys(scope ?? {}).filter((key) =>
    key.startsWith('--paper-') || key.startsWith('--sheet-')
    || key.endsWith('-narrow') || key === '--headline-margin-inline');

describe('sheets paper', () => {
  it.each(['#ffffff', '#FFFFFF', '#FfFfFf'])(
    'turns a %s background into the warm paper',
    (background) => {
      const styles = sheets(customize((value) => {
        value.colors.background = background;
      }));
      expect(styles.root['--paper-color-surface']).toBe('#faf6ec');
      expect(styles.root['--color-surface']).toBe(background);
    },
  );

  it('keeps an authored paper such as designer-tag', () => {
    const designer = TEMPLATES.find((preset) => preset.id === 'designer-tag')!;
    const customization = customize((value) => {
      value.colors = { ...designer.customization.colors };
    });
    expect(customization.colors.background).toBe('#f4f1ea');
    const styles = sheets(customization);
    expect(styles.root['--paper-color-surface']).toBe('#f4f1ea');
    expect(styles.root['--paper-color-body']).toBe(styles.root['--color-body']);
  });

  it('keeps an authored dark background below OKLCH 0.5 as the paper', () => {
    const customization = customize((value) => {
      value.colors = {
        primary: '#e8e8e8',
        text: '#dddddd',
        background: '#101820',
        accent: '#7fb2ff',
      };
    });
    expect(rgbToOKLCH('#101820').l).toBeLessThan(0.5);
    const light = sheets(customization);
    expect(light.root['--paper-color-surface']).toBe('#101820');
    for (const colorScheme of ['dark', 'system'] as const) {
      const dark = sheets(customization, colorScheme);
      expect(dark.root['--dark-color-surface']).toBe('#101820');
      for (const role of ROLES) {
        expect(dark.root[`--dark-color-${role}`], role)
          .toBe(light.root[`--color-${role}`]);
      }
    }
  });

  it.each(['dark', 'system'] as const)(
    'uses the charcoal paper for %s on a light template',
    (colorScheme) => {
      for (const preset of TEMPLATES) {
        const customization = customize((value) => {
          value.colors = { ...preset.customization.colors };
        });
        const styles = sheets(customization, colorScheme);
        expect(styles.root['--dark-color-surface'], preset.id).toBe('#2a2723');
      }
    },
  );

  it('derives the paper roles against the paper with the same floors', () => {
    for (const preset of TEMPLATES) {
      const root = sheets(customize((value) => {
        value.colors = { ...preset.customization.colors };
      })).root;
      const paper = root['--paper-color-surface']!;
      const at = (role: string, floor: number) =>
        expect(
          contrastRatio(root[`--paper-color-${role}`]!, paper),
          `${preset.id} ${role}`,
        ).toBeGreaterThanOrEqual(floor);
      at('body', 4.5);
      at('heading', 4.5);
      at('meta', 4.5);
      at('link', 4.5);
      at('accent-solid', 3);
      at('rule', 1.5);
    }
  });

  it('keeps the authored roles that print uses', () => {
    const continuous = useResumeStyles(base);
    const paper = sheets(base);
    for (const role of ROLES) {
      expect(paper.root[`--color-${role}`], role)
        .toBe(continuous.root[`--color-${role}`]);
      expect(paper.root[`--paper-color-${role}`], role).toMatch(
        /^#[0-9a-f]{6}$/u,
      );
    }
    expect(paper.root['--paper-color-surface-header']).toBe('#faf6ec');
    expect(paper.root['--paper-color-surface-sidebar']).toBe('#faf6ec');
  });

  it('keeps a tinted header band on its own surface', () => {
    const band = TEMPLATES.find((preset) => preset.id === 'creative-accent')!;
    const customization = customize((value) => {
      value.colors = { ...band.customization.colors };
      value.layout.surfaceTarget = 'header';
    });
    const styles = sheets(customization);
    expect(styles.header?.['--color-surface']).toBe('#fff0eb');
    expect(sheetKeys(styles.header)).toEqual([]);
    expect(styles.root['--paper-color-surface-header']).toBe('#fff0eb');
  });
});

describe('sheets dark surfaces', () => {
  it('lifts a light band to the paper lightness plus 0.07', () => {
    const band = TEMPLATES.find((preset) => preset.id === 'creative-accent')!;
    const customization = customize((value) => {
      value.colors = { ...band.customization.colors };
      value.layout.surfaceTarget = 'header';
    });
    const surface = sheets(customization, 'dark')
      .header!['--dark-color-surface']!;
    const paper = rgbToOKLCH('#2a2723').l;
    expect(rgbToOKLCH(surface).l).toBeCloseTo(paper + 0.07, 2);
  });

  it('lifts the executive-band navy to at least the paper plus 0.07', () => {
    const preset = TEMPLATES.find((item) => item.id === 'executive-band')!;
    const customization = customize((value) => {
      value.colors = { ...preset.customization.colors };
      value.layout.surfaceTarget = 'header';
    });
    const surface = sheets(customization, 'dark')
      .header!['--dark-color-surface']!;
    const authored = rgbToOKLCH('#16273d');
    const lifted = rgbToOKLCH(surface);
    expect(lifted.l).toBeCloseTo(rgbToOKLCH('#2a2723').l + 0.07, 2);
    expect(lifted.l).toBeGreaterThan(authored.l);
    expect(Math.abs(lifted.h - authored.h)).toBeLessThan(0.05);
  });
});

describe('sheets overrides', () => {
  it('floors every type size at a 16 px base and keeps the multiples', () => {
    const root = sheets(base).root;
    expect(root['--sheet-fs-base']).toBe('16px');
    expect(root['--sheet-fs-body']).toBe('16px');
    expect(root['--sheet-fs-title']).toBe('16px');
    expect(root['--sheet-fs-subtitle']).toBe('16px');
    expect(root['--sheet-fs-name']).toBe('26px');
    expect(root['--sheet-fs-name-narrow']).toBe('26px');
    expect(root['--sheet-fs-headline']).toBe(`${16 * 1.15}px`);
    expect(root['--sheet-fs-heading']).toBe(`${16 * 1.1}px`);
    expect(root['--sheet-fs-meta']).toBe(`${16 * 0.9}px`);
    expect(root['--fs-body']).toBe('13px');
    expect(root['--fs-name']).toBe('26px');
  });

  it('keeps a base above 16 px', () => {
    const root = sheets(customize((value) => {
      value.font.baseSizePx = 18;
    })).root;
    for (const size of SIZES) {
      expect(root[`--sheet-fs-${size}`], size).toBe(root[`--fs-${size}`]);
    }
  });

  it.each([
    [13, '26px', '26px'],
    [14, '28px', '27.2px'],
    [15, '30px', '27.2px'],
    [16, '32px', '27.2px'],
    [18, '36px', '30.6px'],
  ] as const)(
    'caps the name of a %i px base at the template size and 1.7em narrow',
    (baseSizePx, wide, narrow) => {
      const root = sheets(customize((value) => {
        value.font.baseSizePx = baseSizePx;
      })).root;
      expect(root['--sheet-fs-name']).toBe(wide);
      expect(root['--sheet-fs-name-narrow']).toBe(narrow);
      expect(root['--fs-name']).toBe(`${baseSizePx * 2}px`);
    },
  );

  it('emits no name cap outside sheets', () => {
    const root = useResumeStyles(base).root;
    expect(root['--sheet-fs-name-narrow']).toBeUndefined();
  });

  it('raises the line height to at least 1.5', () => {
    expect(sheets(base).root['--lh-body']).toBe('1.5');
    expect(sheets(customize((value) => {
      value.spacing.lineHeight = 1.65;
    })).root['--lh-body']).toBe('1.65');
  });

  it('start-aligns justified text below the narrow width', () => {
    expect(sheets(customize((value) => {
      value.font.textAlign = 'justify';
    })).root['--body-align-narrow']).toBe('start');
    expect(sheets(base).root['--body-align-narrow']).toBe('left');
    expect(sheets(customize((value) => {
      value.font.textAlign = 'justify';
    })).root['--body-align']).toBe('justify');
  });

  it('keeps the headline measure under its header alignment', () => {
    expect(sheets(base).root['--headline-margin-inline']).toBe('auto');
    expect(sheets(customize((value) => {
      value.header!.align = 'left';
    })).root['--headline-margin-inline']).toBe('0');
  });

  it('emits nothing new outside sheets', () => {
    for (const colorScheme of [undefined, 'dark', 'system'] as const) {
      const plain = useResumeStyles(base, 'en', colorScheme);
      const explicit = useResumeStyles(base, 'en', colorScheme, {
        sheets: false,
      });
      expect(explicit).toEqual(plain);
      expect(sheetKeys(plain.root)).toEqual([]);
      expect(plain.root['--lh-body']).toBe('1.3');
      expect(plain.root['--fs-body']).toBe('13px');
    }
  });

  it('turns contact details into rows only in sheets mode', () => {
    const document = JSON.parse(readFileSync(
      resolve(
        import.meta.dirname,
        '../../../../packages/schema/fixtures/minimal.json',
      ),
      'utf8',
    )) as Resume;
    const model = (mode: 'continuous' | 'paged' | 'sheets') =>
      resolveRenderModel(document, { lng: 'en', mode });
    expect(model('sheets').header.detailsLayout).toBe('rows');
    expect(model('continuous').header.detailsLayout).not.toBe('rows');
    expect(model('paged').header.detailsLayout).not.toBe('rows');
    expect(sheetKeys(model('continuous').styles.root)).toEqual([]);
    expect(sheetKeys(model('paged').styles.root)).toEqual([]);
    expect(sheetKeys(model('sheets').styles.root).length).toBeGreaterThan(0);
  });

  it('carries the color scheme into the sheets model but not paged', () => {
    const document = JSON.parse(readFileSync(
      resolve(
        import.meta.dirname,
        '../../../../packages/schema/fixtures/minimal.json',
      ),
      'utf8',
    )) as Resume;
    expect(resolveRenderModel(document, {
      lng: 'en',
      mode: 'sheets',
      colorScheme: 'system',
    }).colorScheme).toBe('system');
    expect(resolveRenderModel(document, {
      lng: 'en',
      mode: 'paged',
      colorScheme: 'dark',
    }).colorScheme).toBeUndefined();
  });
});
