import type { Customization } from '@aboutme/schema';
import { TEMPLATES } from '@aboutme/schema/templates';
import { describe, expect, it } from 'vitest';

import { contrastRatio } from '../../app/components/resume/clampContrast';
import {
  darkColors,
  isDarkBackground,
  oklchToGamutHex,
} from '../../app/components/resume/darkPalette';
import {
  effectiveSurfaceTarget,
  useResumeStyles,
} from '../../app/components/resume/useResumeStyles';

// docs/design/public-page-theme.md, "Template mapping": the ground, body,
// heading, and link on the ground for each preset's own colors, plus the
// tinted region's dark surface and where it sits.
const MAPPING: Record<
  string,
  readonly [string, string, string, string, string?, string?]
> = {
  'academic-dense': ['#12171c', '#d9dfe4', '#aec9e6', '#aec9e6'],
  'ats-plain': ['#161616', '#dedede', '#ebebeb', '#ebebeb'],
  'classic-serif': ['#151617', '#dadee6', '#dbe0e7', '#92b0e5'],
  'consulting-formal': ['#11171d', '#d8dfe5', '#9fbfe6', '#6a9dd5'],
  'creative-accent':
    ['#1c1411', '#e4dcd8', '#dc5330', '#dc5330', '#352019', 'header'],
  'designer-tag': ['#151711', '#e0dddb', '#acba99', '#778d4e'],
  'editorial-wide': ['#191512', '#e1ddd7', '#cbb9a7', '#b4805a'],
  'elegant-serif-two':
    ['#1c1315', '#e3ddd7', '#db8b99', '#db8b99', '#2f2512', 'sidebar'],
  'engineer-compact': ['#10171c', '#d8dfe6', '#92bcd4', '#5a9db9'],
  'executive-band':
    ['#13161b', '#d8dfe8', '#b4c9e6', '#a6753b', '#1d2f45', 'header'],
  'government-formal': ['#161616', '#dedede', '#ebebeb', '#ebebeb'],
  'graduate-friendly':
    ['#1c1410', '#e3ddd7', '#c88262', '#bc6934', '#322313', 'header'],
  'high-contrast': ['#161616', '#dedede', '#ebebeb', '#5085ff'],
  'international-lang':
    ['#171615', '#e1ddd9', '#e0dcd9', '#a79483', '#2a261d', 'header'],
  'minimal-air': ['#151617', '#dadee5', '#dbdee4', '#dbdee4'],
  'modern-sidebar':
    ['#0f171b', '#d5e0e6', '#88b7ce', '#4090a3', '#1b292f', 'sidebar'],
  'mono-print': ['#161616', '#dedede', '#ebebeb', '#ebebeb'],
  'nordic-muted':
    ['#12171a', '#d7dfe8', '#8fa8bd', '#63859b', '#24272a', 'sidebar'],
  'one-page-tight': ['#131619', '#d8dfe6', '#aec0cf', '#8ba6b9'],
  'startup-bold': ['#151617', '#d8dee8', '#e8ebf0', '#e8ebf0'],
};

const HEX = /^#[0-9a-f]{6}$/u;

const storedCustomization = (
  preset: (typeof TEMPLATES)[number],
  columns: 1 | 2,
): Customization => {
  const { placement: _placement, sidebarSectionTypes: _types, ...layout }
    = preset.customization.layout;
  return {
    ...structuredClone(preset.customization),
    layout: { ...layout, columns, sections: { main: [], sidebar: [] } },
  };
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

const darkScope = (
  scope: Record<string, string>,
): Record<string, string> =>
  Object.fromEntries(
    ROLES.map((role) => [role, scope[`--dark-color-${role}`]!]),
  );

describe('dark palette rule', () => {
  it('covers every preset the design maps', () => {
    expect(TEMPLATES.map((preset) => preset.id).sort()).toEqual(
      Object.keys(MAPPING).sort(),
    );
  });

  describe.each(TEMPLATES.map((preset) => [preset.id, preset] as const))(
    '%s',
    (id, preset) => {
      it.each([1, 2] as const)('maps the design values in %i column(s)',
        (columns) => {
          const customization = storedCustomization(preset, columns);
          const styles = useResumeStyles(customization, 'en', 'dark');
          const [ground, body, heading, link, region, where] = MAPPING[id]!;
          expect(styles.root['--dark-color-surface']).toBe(ground);
          expect(styles.root['--dark-color-body']).toBe(body);
          expect(styles.root['--dark-color-heading']).toBe(heading);
          expect(styles.root['--dark-color-link']).toBe(link);
          const target = effectiveSurfaceTarget(customization);
          const scope = target === 'header'
            ? styles.header
            : target === 'sidebar'
              ? styles.sidebar
              : undefined;
          if (region === undefined || where !== target) {
            expect(scope).toBeUndefined();
            return;
          }
          expect(scope?.['--dark-color-surface']).toBe(region);
        });

      it.each([1, 2] as const)(
        'meets every contrast floor on every surface in %i column(s)',
        (columns) => {
          const customization = storedCustomization(preset, columns);
          const styles = useResumeStyles(customization, 'en', 'dark');
          const target = effectiveSurfaceTarget(customization);
          const scopes: ReadonlyArray<readonly [string, Record<string, string>]>
            = [
              ['page', styles.root],
              ...(target === 'header' && styles.header
                ? [['header', styles.header] as const]
                : []),
              ...(target === 'sidebar' && styles.sidebar
                ? [['sidebar', styles.sidebar] as const]
                : []),
            ];
          for (const [label, raw] of scopes) {
            const roles = darkScope(raw);
            const surface = roles.surface!;
            const at = (role: string, floor: number): void => {
              expect(
                contrastRatio(roles[role]!, surface),
                `${id} ${label} ${role}`,
              ).toBeGreaterThanOrEqual(floor);
            };
            at('body', 4.5);
            at('heading', 4.5);
            at('meta', 4.5);
            at('link', 4.5);
            at('accent-text', 4.5);
            // The name uses the heading role at the 3:1 large-text floor.
            at('heading', 3);
            at('accent-solid', 3);
            expect(
              contrastRatio(roles['accent-solid']!, roles.track!),
              `${id} ${label} level fill on track`,
            ).toBeGreaterThanOrEqual(3);
            expect(
              contrastRatio(roles['on-accent']!, roles['accent-solid']!),
              `${id} ${label} tag label`,
            ).toBeGreaterThanOrEqual(4.5);
            at('rule', 1.5);
            // The measured minimums across all twenty presets on every
            // surface (docs/design/public-page-theme.md, "Template mapping").
            at('body', 10.1);
            at('meta', 6.4);
            // Level and tag fills are the accent-solid role, measured against
            // the surface they sit on, not the empty track.
            at('accent-solid', 3.5);
          }
        },
      );

      it('writes only #rrggbb values and leaves the light roles alone', () => {
        const customization = storedCustomization(preset, 2);
        const light = useResumeStyles(customization);
        const dark = useResumeStyles(customization, 'en', 'system');
        for (const [name, value] of Object.entries(dark.root)) {
          if (name.startsWith('--dark-color-')) expect(value).toMatch(HEX);
          else if (name.startsWith('--color-')) {
            expect(value, name).toBe(light.root[name]);
          }
        }
        expect(dark.header?.['--header-padding'])
          .toBe(light.header?.['--header-padding']);
      });
    },
  );

  it('adds no dark custom property to light output', () => {
    for (const preset of TEMPLATES) {
      const styles = useResumeStyles(storedCustomization(preset, 2));
      for (const scope of [styles.root, styles.header, styles.sidebar]) {
        expect(
          Object.keys(scope ?? {}).filter((key) => key.includes('dark')),
        ).toEqual([]);
      }
    }
  });

  it('writes a dark role set at the article, header band, and sidebar', () => {
    const header = TEMPLATES.find((preset) => preset.id === 'creative-accent')!;
    const sidebar = TEMPLATES.find((preset) => preset.id === 'modern-sidebar')!;
    const band = useResumeStyles(storedCustomization(header, 1), 'en', 'dark');
    expect(Object.keys(band.header!).filter((key) =>
      key.startsWith('--dark-color-')).sort()).toEqual(
      ROLES.map((role) => `--dark-color-${role}`).sort(),
    );
    const side = useResumeStyles(storedCustomization(sidebar, 2), 'en', 'dark');
    expect(Object.keys(side.sidebar!).filter((key) =>
      key.startsWith('--dark-color-'))).toHaveLength(ROLES.length);
    expect(side.header).toBeUndefined();
  });

  it('keeps a heading on a dark band in its hue instead of white', () => {
    const preset = TEMPLATES.find((item) => item.id === 'executive-band')!;
    const styles = useResumeStyles(
      storedCustomization(preset, 1),
      'en',
      'dark',
    );
    expect(styles.header!['--dark-color-heading']).not.toBe('#ffffff');
    expect(styles.header!['--dark-color-heading']).toBe('#b4c9e6');
    // The light scheme keeps its shortcut: failing ink on a dark band is white.
    expect(styles.header!['--color-heading']).toBe('#ffffff');
  });
});

describe('dark source colors', () => {
  const colors: Customization['colors'] = {
    primary: '#16273d',
    text: '#1f2933',
    background: '#ffffff',
  };

  it('returns an already dark background as authored', () => {
    const authored = { ...colors, background: '#101820', text: '#e8e8e8' };
    expect(isDarkBackground(authored)).toBe(true);
    expect(darkColors(authored)).toBe(authored);
  });

  it('tones the ground by the background tint, else by the primary', () => {
    const neutral = darkColors(colors);
    const tinted = darkColors({ ...colors, background: '#fbf3e6' });
    expect(neutral.background).toMatch(HEX);
    expect(tinted.background).not.toBe(neutral.background);
    // A tinted paper carries a trace of its warm hue.
    const [red, , blue] = [1, 3, 5].map((index) =>
      Number.parseInt(tinted.background.slice(index, index + 2), 16));
    expect(red!).toBeGreaterThan(blue!);
  });

  it('lifts a dark ink and keeps an absent accent or surface absent', () => {
    const result = darkColors(colors);
    expect(contrastRatio(result.primary, result.background))
      .toBeGreaterThanOrEqual(4.5);
    expect(result).not.toHaveProperty('accent');
    expect(result).not.toHaveProperty('surface');
  });

  it('keeps an authored dark surface a band above the ground', () => {
    const result = darkColors({ ...colors, surface: '#000000' });
    expect(result.surface).toMatch(HEX);
    expect(contrastRatio(result.surface!, result.background))
      .toBeGreaterThan(1);
  });

  it('never rewrites the authored colors', () => {
    const before = structuredClone(colors);
    darkColors(colors);
    expect(colors).toEqual(before);
  });

  it('clips an out-of-gamut color into sRGB by lowering chroma', () => {
    expect(oklchToGamutHex({ l: 0.9, c: 0.4, h: 2 })).toMatch(HEX);
    expect(oklchToGamutHex({ l: 0.2, c: 0.4, h: 4 })).toMatch(HEX);
    expect(oklchToGamutHex({ l: 0.5, c: 0, h: 0 })).toBe('#636363');
  });
});
