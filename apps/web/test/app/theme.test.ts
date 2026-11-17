import type { Resume } from '@aboutme/schema';
import { mount } from '@vue/test-utils';
import { readFileSync, readdirSync } from 'node:fs';
import { resolve } from 'node:path';
import { afterEach, describe, expect, it } from 'vitest';

import ResumeDocument from '../../app/components/resume/ResumeDocument.vue';
import { FIXED_PHOTO_DATA_URL } from '../../app/pages/_harness/photo-fixture';

const webRoot = resolve(import.meta.dirname, '../..');
const workspaceRoot = resolve(webRoot, '../..');
const themePath = resolve(webRoot, 'app/assets/css/theme.css');
const basePath = resolve(webRoot, 'app/assets/css/base.css');
const LIGHT = ':root';
const DARK = 'html[data-theme="dark"]';

type Theme = 'light' | 'dark';

const COLORS: Record<Theme, Record<string, string>> = {
  light: {
    '--background': '#ffffff',
    '--foreground': '#1a1650',
    '--card-foreground': '#1a1650',
    '--popover-foreground': '#1a1650',
    '--secondary-foreground': '#1a1650',
    '--accent-foreground': '#1a1650',
    '--muted-foreground': '#55517d',
    '--card': '#ffffff',
    '--popover': '#ffffff',
    '--brand': '#594bad',
    '--primary': '#594bad',
    '--brand-hover': '#4c3c9c',
    '--primary-hover': '#4c3c9c',
    '--brand-foreground': '#ffffff',
    '--primary-foreground': '#ffffff',
    '--brand-text': '#4f3da5',
    '--brand-panel': '#594bad',
    '--seal': '#de1b5a',
    '--seal-hover': '#c7134d',
    '--seal-foreground': '#ffffff',
    '--seal-text': '#c7134d',
    '--link': '#4336c9',
    '--destructive': '#914d00',
    '--surface-destructive': '#f7f3ed',
    '--surface-mist': '#f1effa',
    '--muted': '#f1effa',
    '--editor-canvas': '#f1effa',
    '--surface-blush': '#ece2ff',
    '--surface-sun': '#fff2c2',
    '--accent': '#f1effa',
    '--secondary': '#f1effa',
    '--panel': '#1a1650',
    '--panel-foreground': '#ffffff',
    '--panel-muted': '#d9d6f2',
    '--footer': '#1a1650',
    '--border': '#e3e0f2',
    '--hairline': 'rgba(26,22,80,0.14)',
    '--hairline-inverse': 'rgba(255,255,255,0.2)',
    '--input': '#7f7aae',
    '--control': '#7f7aae',
    '--ring': '#1a1650',
    '--ring-inverse': '#ffffff',
    '--coin': '#ffd23f',
    '--coin-edge': '#e9b416',
    '--coin-ink': '#1a1650',
    '--chart-bar': '#594bad',
    '--overlay': 'rgba(26,22,80,0.55)',
    '--logo-mark': '#de1b5a',
    '--logo-mark-inverse': '#ff7aa2',
  },
  dark: {
    '--background': '#0e0a0d',
    '--foreground': '#d3cad0',
    '--card-foreground': '#d3cad0',
    '--popover-foreground': '#d3cad0',
    '--secondary-foreground': '#d3cad0',
    '--accent-foreground': '#d3cad0',
    '--muted-foreground': '#a3959f',
    '--card': '#1b1419',
    '--popover': '#1b1419',
    '--brand': '#7267c0',
    '--primary': '#7267c0',
    '--brand-hover': '#675bb3',
    '--primary-hover': '#675bb3',
    '--brand-foreground': '#ffffff',
    '--primary-foreground': '#ffffff',
    '--brand-text': '#aea7ed',
    '--brand-panel': '#382961',
    '--seal': '#c42d58',
    '--seal-hover': '#b42650',
    '--seal-foreground': '#ffffff',
    '--seal-text': '#e2788f',
    '--link': '#b59bdb',
    '--destructive': '#f0ab72',
    '--surface-destructive': '#2c2020',
    '--surface-mist': '#151015',
    '--muted': '#151015',
    '--editor-canvas': '#151015',
    '--surface-blush': '#1c172f',
    '--surface-sun': '#271a07',
    '--accent': '#251b23',
    '--secondary': '#251b23',
    '--panel': '#25153a',
    '--panel-foreground': '#e7dfe8',
    '--panel-muted': '#c2b6cb',
    '--footer': '#150c1c',
    '--border': '#2e242c',
    '--hairline': 'rgba(211,202,208,0.14)',
    '--hairline-inverse': 'rgba(255,255,255,0.2)',
    '--input': '#7e6d7a',
    '--control': '#7e6d7a',
    '--ring': '#cdbcd9',
    '--ring-inverse': '#ffffff',
    '--coin': '#ffd23f',
    '--coin-edge': '#e9b416',
    '--coin-ink': '#1a1650',
    '--chart-bar': '#8f85d7',
    '--overlay': 'rgba(4,2,3,0.72)',
    '--logo-mark': '#e2788f',
    '--logo-mark-inverse': '#e2788f',
  },
};

const FIXED = {
  '--radius': '10px',
  '--radius-sheet': '2px',
  '--radius-dialog': '14px',
  '--radius-card': '22px',
  '--radius-panel': '28px',
  '--space-module': '8px',
  '--space-dialog-title': '6px',
  '--space-field': '16px',
  '--space-dialog': '24px',
  '--space-section': '80px',
  '--space-section-lg': '112px',
  '--paper': '#ffffff',
  '--paper-ink': '#171a18',
  '--paper-muted': '#5f6763',
  '--paper-hover': '#f0f2f1',
  '--paper-seal-ground': '#ffffff',
  '--paper-seal-ink': '#c7134d',
  '--shadow-paper-hero':
    '0 36px 56px -28px rgba(0,0,0,0.5),0 2px 6px rgba(0,0,0,0.16)',
  '--font-chrome':
    '\'Plus Jakarta Sans\',\'Be Vietnam Pro\',system-ui,sans-serif',
  '--font-paper': '\'Be Vietnam Pro\',\'Inter\',system-ui,sans-serif',
} as const;

const SHADOWS: Record<Theme, Record<string, string>> = {
  light: {
    '--shadow-paper':
      '0 1px 2px rgba(23,26,24,0.06),0 12px 32px rgba(23,26,24,0.1)',
    '--shadow-float': '0 16px 24px -14px rgba(26,22,80,0.55)',
    '--shadow-card': '0 32px 48px -32px rgba(26,22,80,0.45)',
    '--shadow-dialog':
      '0 32px 64px -24px rgba(26,22,80,0.5),0 2px 8px rgba(26,22,80,0.12)',
  },
  dark: {
    '--shadow-paper': '0 1px 2px rgba(0,0,0,0.4),0 12px 32px rgba(0,0,0,0.5)',
    '--shadow-float': '0 16px 24px -14px rgba(0,0,0,0.7)',
    '--shadow-card': '0 32px 48px -32px rgba(0,0,0,0.75)',
    '--shadow-dialog':
      '0 32px 64px -24px rgba(0,0,0,0.8),0 2px 8px rgba(0,0,0,0.4)',
  },
};

const RETIRED = new RegExp([
  '(?<!\\w)(?:brand-(?:blue|deep-blue|indigo|jade|ochre)',
  'surface-(?:blue|indigo|sand)',
  'radius-feature',
  'shadow-(?:product|primary|cta))(?![\\w-])',
  '--gradient-[\\w-]+',
].join('|'), 'gu');

const GROUNDS = [
  ['background', ['--background']],
  ['card, popover', ['--card', '--popover']],
  ['mist, editor canvas', ['--surface-mist', '--muted', '--editor-canvas']],
  ['blush', ['--surface-blush']],
  ['sun', ['--surface-sun']],
  ['accent, secondary', ['--accent', '--secondary']],
  ['surface-destructive', ['--surface-destructive']],
] as const;

const COLUMNS = [
  ['--foreground', 4.5], ['--muted-foreground', 4.5], ['--link', 4.5],
  ['--brand-text', 4.5], ['--seal-text', 4.5], ['--destructive', 4.5],
  ['--control', 3], ['--brand', 3], ['--seal', 3],
] as const;

type Row = readonly [
  foreground: number, muted: number, link: number, brandText: number,
  sealText: number, destructive: number, control: number, mainFill: number,
  sealFill: number,
];

const CONTRAST: Record<Theme, readonly Row[]> = {
  light: [
    [16.48, 7.34, 8.05, 8.26, 5.79, 6.43, 3.96, 6.92, 4.76],
    [16.48, 7.34, 8.05, 8.26, 5.79, 6.43, 3.96, 6.92, 4.76],
    [14.49, 6.46, 7.08, 7.26, 5.09, 5.66, 3.49, 6.09, 4.19],
    [13.25, 5.91, 6.47, 6.64, 4.65, 5.17, 3.19, 5.57, 3.83],
    [14.70, 6.55, 7.18, 7.37, 5.16, 5.74, 3.54, 6.17, 4.25],
    [14.49, 6.46, 7.08, 7.26, 5.09, 5.66, 3.49, 6.09, 4.19],
    [14.91, 6.64, 7.28, 7.47, 5.24, 5.82, 3.59, 6.26, 4.31],
  ],
  dark: [
    [12.29, 6.89, 8.12, 8.94, 6.85, 10.09, 4.08, 4.14, 3.62],
    [11.31, 6.34, 7.47, 8.23, 6.30, 9.28, 3.75, 3.81, 3.33],
    [11.76, 6.59, 7.77, 8.55, 6.55, 9.65, 3.90, 3.95, 3.46],
    [10.82, 6.07, 7.15, 7.87, 6.03, 8.88, 3.59, 3.64, 3.19],
    [10.61, 5.95, 7.01, 7.72, 5.91, 8.71, 3.52, 3.57, 3.12],
    [10.42, 5.84, 6.89, 7.58, 5.81, 8.55, 3.46, 3.51, 3.07],
    [9.84, 5.51, 6.50, 7.16, 5.48, 8.07, 3.26, 3.31, 2.89],
  ],
};

const DOCUMENTED_EXCEPTIONS = new Set([
  'dark --seal on --surface-destructive',
]);

type Pair = readonly [
  foreground: string, ground: string, ratio: number, minimum: number,
];

const NOTES: Record<Theme, readonly Pair[]> = {
  light: [
    ['--brand-foreground', '--brand', 6.92, 4.5],
    ['--brand-foreground', '--brand-hover', 8.63, 4.5],
    ['--seal-foreground', '--seal', 4.76, 4.5],
    ['--seal-foreground', '--seal-hover', 5.79, 4.5],
    ['--panel-foreground', '--brand-panel', 6.92, 4.5],
    ['--panel-muted', '--brand-panel', 4.89, 4.5],
    ['--coin-ink', '--panel-foreground', 16.48, 4.5],
    ['--panel-foreground', '--panel', 16.48, 4.5],
    ['--panel-muted', '--panel', 11.65, 4.5],
    ['--panel-foreground', '--footer', 16.48, 4.5],
    ['--ring', '--background', 16.48, 3],
    ['--ring', '--card', 16.48, 3],
    ['--ring', '--surface-mist', 14.49, 3],
    ['--chart-bar', '--card', 6.92, 3],
    ['--coin-ink', '--coin', 11.41, 4.5],
  ],
  dark: [
    ['--brand-foreground', '--brand', 4.75, 4.5],
    ['--brand-foreground', '--brand-hover', 5.64, 4.5],
    ['--seal-foreground', '--seal', 5.44, 4.5],
    ['--seal-foreground', '--seal-hover', 6.30, 4.5],
    ['--panel-foreground', '--brand-panel', 9.72, 4.5],
    ['--panel-muted', '--brand-panel', 6.54, 4.5],
    ['--coin-ink', '--panel-foreground', 12.65, 4.5],
    ['--panel-foreground', '--panel', 12.93, 4.5],
    ['--panel-muted', '--panel', 8.70, 4.5],
    ['--panel-foreground', '--footer', 14.64, 4.5],
    ['--ring', '--background', 11.04, 3],
    ['--ring', '--card', 10.16, 3],
    ['--ring', '--surface-mist', 10.56, 3],
    ['--chart-bar', '--card', 5.60, 3],
    ['--coin-ink', '--coin', 11.41, 4.5],
    ['--brand', '--panel', 3.54, 3],
    ['--brand', '--footer', 4.01, 3],
  ],
};

afterEach(() => {
  delete document.documentElement.dataset.theme;
});

describe('application theme', () => {
  for (const theme of ['light', 'dark'] as const) {
    it(`defines the token page colors in ${theme}`, () => {
      const tokens = themeTokens(theme);
      for (const [name, value] of Object.entries(COLORS[theme])) {
        expect(resolveValue(name, tokens), name).toBe(value);
      }
      for (const [name, value] of Object.entries(SHADOWS[theme])) {
        expect(resolveValue(name, tokens), name).toBe(value);
      }
      for (const [name, value] of Object.entries(FIXED)) {
        expect(resolveValue(name, tokens), name).toBe(value);
      }
    });

    it(`meets the token page contrast table in ${theme}`, () => {
      const tokens = themeTokens(theme);
      GROUNDS.forEach(([label, grounds], row) => {
        const expected = CONTRAST[theme][row]!;
        for (const ground of grounds) {
          const groundHex = resolveValue(ground, tokens);
          COLUMNS.forEach(([token, minimum], column) => {
            const ratio = contrast(resolveValue(token, tokens), groundHex);
            const name = `${theme} ${token} on ${ground}`;
            if (!DOCUMENTED_EXCEPTIONS.has(name)) {
              expect(ratio, `${name} (${label})`)
                .toBeGreaterThanOrEqual(minimum);
            }
            expectRatio(ratio, expected[column]!, `${name} (${label})`);
          });
        }
      });
    });

    it(`meets the token page fill, hero, ring and chart ratios in ${theme}`,
      () => {
        const tokens = themeTokens(theme);
        for (const [foreground, ground, expected, minimum] of NOTES[theme]) {
          const ratio = contrast(
            resolveValue(foreground, tokens), resolveValue(ground, tokens),
          );
          expect(ratio, `${foreground} on ${ground}`)
            .toBeGreaterThanOrEqual(minimum);
          expectRatio(ratio, expected, `${foreground} on ${ground}`);
        }
      });

    it(`keeps seal words readable on the ticket fill in ${theme}`, () => {
      const tokens = themeTokens(theme);
      const ink = resolveValue('--seal-text', tokens);
      const fill = mixHex(ink, resolveValue('--card', tokens), 0.07);
      expect(contrast(ink, fill)).toBeGreaterThanOrEqual(4.5);
    });
  }

  it('keeps the main fill off light panel and footer grounds', () => {
    const tokens = themeTokens('light');
    for (const ground of ['--panel', '--footer']) {
      const ratio = contrast(
        resolveValue('--brand', tokens), resolveValue(ground, tokens),
      );
      expect(ratio, ground).toBeLessThan(3);
      expectRatio(ratio, 2.38, ground);
    }
  });

  it('keeps the seal apart from the main color', () => {
    for (const theme of ['light', 'dark'] as const) {
      const block = blockDeclarations(css(), theme === 'light' ? LIGHT : DARK);
      for (const name of [
        '--seal', '--seal-hover', '--seal-foreground', '--seal-text',
      ]) {
        expect(block[name] ?? '', `${theme} ${name}`)
          .not.toMatch(/var\(--(?:brand|primary)/u);
      }
      const tokens = themeTokens(theme);
      expect(resolveValue('--seal', tokens))
        .not.toBe(resolveValue('--brand', tokens));
    }
  });

  it('keeps paper and fixed tokens out of the dark block', () => {
    const dark = blockDeclarations(css(), DARK);
    for (const token of [
      ...Object.keys(FIXED),
      '--brand-foreground', '--seal-foreground', '--coin', '--coin-edge',
      '--coin-ink',
    ]) {
      expect(dark[token], token).toBeUndefined();
    }
  });

  it('steps the card and panel radii at 48rem and 64rem', () => {
    expect(mediaTokens('48rem')).toEqual({
      '--radius-card': '26px', '--radius-panel': '36px',
    });
    expect(mediaTokens('64rem')).toEqual({
      '--radius-card': '28px', '--radius-panel': '44px',
    });
  });

  it('names no retired token in the app or its render workers', () => {
    const names = [
      ...sourceFiles(resolve(webRoot, 'app')),
      ...sourceFiles(resolve(webRoot, 'server')),
    ].flatMap((path) => [...readFileSync(path, 'utf8').matchAll(RETIRED)]
      .map((match) => `${path.slice(webRoot.length + 1)} ${match[0]}`));

    expect(names).toEqual([]);
    expect(css()).not.toMatch(/gradient\(/u);
  });

  it('maps the chrome font, colors, radii, and type scale', () => {
    const theme = blockDeclarations(css(), '@theme inline');

    expect(theme['--font-sans']).toBe('var(--font-chrome)');
    for (const name of [
      'seal', 'seal-hover', 'seal-foreground', 'seal-text', 'link', 'brand',
      'brand-hover', 'brand-panel',
      'brand-foreground', 'brand-text', 'surface-mist', 'surface-blush',
      'surface-sun', 'surface-destructive', 'panel', 'panel-foreground',
      'panel-muted', 'footer', 'hairline', 'control', 'coin', 'coin-ink',
      'chart-bar', 'overlay', 'editor-canvas', 'primary-hover', 'paper',
      'paper-ink', 'paper-muted', 'paper-hover',
    ]) {
      expect(theme[`--color-${name}`], name).toBe(`var(--${name})`);
    }
    for (const retired of ['pink', 'orange', 'cyan', 'purple']) {
      expect(theme[`--color-brand-${retired}`]).toBeUndefined();
    }
    expect(theme['--color-surface-pink']).toBeUndefined();
    expect(theme['--radius-sm']).toBe('calc(var(--radius) - 4px)');
    expect(theme['--radius-md']).toBe('var(--radius)');
    expect(theme['--radius-lg']).toBe('var(--radius)');
    expect(theme['--radius-xl']).toBe('calc(var(--radius) + 4px)');
    const scale = {
      'xs': ['0.75rem', '1.5'],
      'sm': ['0.875rem', '1.5'],
      'base': ['0.9375rem', '1.5'],
      'md': ['1rem', '1.6'],
      'lg': ['1.25rem', '1.4'],
      'xl': ['1.5rem', '1.35'],
      '2xl': ['2rem', '1.3'],
      '3xl': ['2.5rem', '1.3'],
      '4xl': ['3rem', '1.3'],
      '6xl': ['3.75rem', '1.3'],
    } as const;
    for (const [step, [size, line]] of Object.entries(scale)) {
      expect(theme[`--text-${step}`], step).toBe(size);
      expect(theme[`--text-${step}--line-height`], step).toBe(line);
    }
    expect(theme['--text-5xl']).toBeUndefined();
    expect(css()).not.toContain('--shadow-paper: var(--shadow-paper)');
    expect(css()).not.toContain('--radius-sheet: var(--radius-sheet)');
  });

  it(
    'keeps direct seal color use inside the public mark and Publish variant',
    () => {
      const appRoot = resolve(webRoot, 'app');
      const consumers = sourceFiles(appRoot)
        .filter((path) => path !== themePath)
        .filter((path) => {
          const source = readFileSync(path, 'utf8');
          return (
            source.includes('var(--seal')
            || /\b(?:bg|text|border)-seal\b/.test(source)
          );
        })
        .map((path) => path.slice(webRoot.length + 1))
        .sort();

      expect(consumers).toEqual([
        'app/components/app/AppLogo.vue',
        'app/components/app/AppSeal.vue',
        'app/components/ui/button/index.ts',
      ]);
      expect(
        readFileSync(
          resolve(webRoot, 'app/components/app/StateMark.vue'),
          'utf8',
        ),
      ).toMatch(/<AppSeal[\s\S]*size="mark"/);
      expect(
        readFileSync(
          resolve(webRoot, 'app/components/ui/button/index.ts'),
          'utf8',
        ),
      ).toContain(
        'bg-seal font-bold text-seal-foreground hover:bg-seal-hover',
      );
    },
  );

  it('scopes .paper-surface to the paper tokens and the paper face', () => {
    const scope = blockDeclarations(css(), '.paper-surface');
    const plain = plainDeclarations(css(), '.paper-surface');

    expect(scope['--foreground']).toBe('var(--paper-ink)');
    expect(scope['--muted-foreground']).toBe('var(--paper-muted)');
    expect(scope['--accent']).toBe('var(--paper-hover)');
    expect(scope['--accent-foreground']).toBe('var(--paper-ink)');
    expect(scope['--ring']).toBe('#1a1650');
    expect(scope['--link']).toBe('#4336c9');
    expect(scope['--brand-text']).toBe('#4f3da5');
    expect(scope['--seal-text']).toBe('#c7134d');
    expect(scope['--font-chrome']).toBe('var(--font-paper)');
    expect(plain['background-color']).toBe('var(--paper)');
    expect(plain['color']).toBe('var(--paper-ink)');
    expect(plain['font-family']).toBe('var(--font-paper)');
    expect(plain['font-weight']).toBe('400');
  });

  it('paints a flat chrome ground in the chrome face', () => {
    const base = readFileSync(basePath, 'utf8');
    const body = plainDeclarations(base, 'html[data-ui="app"] body');

    expect(body['background-color']).toBe('var(--background)');
    expect(body['font-family']).toBe('var(--font-chrome)');
    expect(body['font-weight']).toBe('500');
    expect(body['background-image']).toBeUndefined();
    expect(base).not.toMatch(/gradient|glow/u);
  });

  it('sets selection, scrollbars, and the seal face outside the resume', () => {
    const base = readFileSync(basePath, 'utf8').replaceAll(/\s+/gu, ' ');
    const guard = ':not( :where( .resume-document, .paged-resume, '
      + '.resume-document *, .paged-resume * ) )';

    expect(base).toContain(
      `html[data-ui="app"] body :where(*)${guard}`
      + ':not(:has(.resume-document, .paged-resume))::selection { '
      + 'background-color: var(--coin); color: var(--coin-ink); }',
    );
    expect(base.match(/::selection/gu)).toHaveLength(1);
    expect(base).toContain(
      `html[data-ui="app"] body :where(*)${guard} { `
      + 'scrollbar-color: var(--control) transparent; }',
    );
    expect(base).toContain(
      `html[data-ui="app"] body :where([data-app-seal])${guard} { `
      + 'font-family: var(--font-paper); }',
    );
  });

  it('resets inherited chrome scrollbar and weight at the resume', () => {
    const base = readFileSync(basePath, 'utf8').replaceAll(/\s+/gu, ' ');

    expect(base).toContain(
      'html[data-ui="app"] body :where(.resume-document, .paged-resume) { '
      + 'scrollbar-color: auto; font-weight: 400; }',
    );
  });

  it('has no retained utility consumer of removed palette tokens', () => {
    const consumers = sourceFiles(resolve(webRoot, 'app'))
      .filter((path) => path !== themePath)
      .filter((path) => {
        const source = readFileSync(path, 'utf8');
        return (
          /\b(?:bg|text|border)-(?:positive(?:\/\d+)?|chart-[1-5])\b/
            .test(source)
            || /(?:brand-(?:cyan|purple)|surface-pink)\b/.test(source)
            || /\bbg-black\/\d+\b|\bbg-primary\/\d+\b/.test(source)
            || /selection:bg-primary\b/.test(source)
        );
      })
      .map((path) => path.slice(webRoot.length + 1));

    expect(consumers).toEqual([]);
    expect(css()).not.toMatch(/--positive(?:-foreground)?\s*:/);
    expect(css()).not.toMatch(/--chart-[1-5]\s*:/);
  });

  it('keeps the document background inline under the dark chrome theme', () => {
    const fixture = JSON.parse(
      readFileSync(
        resolve(workspaceRoot, 'packages/schema/fixtures/full.json'),
        'utf8',
      ),
    ) as Resume;
    document.documentElement.dataset.theme = 'dark';

    const wrapper = mount(ResumeDocument, {
      props: {
        document: fixture,
        context: {
          lng: 'en',
          mode: 'continuous',
          photoUrl: FIXED_PHOTO_DATA_URL,
        },
      },
    });
    const sheet = wrapper.element as HTMLElement;

    expect(sheet.style.getPropertyValue('--color-surface')).toBe(
      fixture.customization.colors.background,
    );
  });
});

function css(): string {
  return readFileSync(themePath, 'utf8');
}

function themeTokens(theme: Theme): Record<string, string> {
  const light = blockDeclarations(css(), LIGHT);
  return theme === 'light'
    ? light
    : { ...light, ...blockDeclarations(css(), DARK) };
}

function mediaTokens(width: string): Record<string, string> {
  const source = css();
  const start = source.indexOf(`@media (min-width: ${width}) {`);
  expect(start, width).toBeGreaterThanOrEqual(0);
  return blockDeclarations(source.slice(start), LIGHT);
}

function expectRatio(actual: number, expected: number, name: string): void {
  expect(Math.abs(actual - expected), `${name}: ${actual}`)
    .toBeLessThan(0.0051);
}

function blockDeclarations(
  source: string,
  selector: string,
): Record<string, string> {
  const start = source.indexOf(`${selector} {`);
  expect(start, `missing ${selector} block`).toBeGreaterThanOrEqual(0);
  const bodyStart = source.indexOf('{', start) + 1;
  const bodyEnd = source.indexOf('}', bodyStart);
  const declarations: Record<string, string> = {};

  for (const match of source
    .slice(bodyStart, bodyEnd)
    .matchAll(/(--[\w-]+)\s*:\s*([^;]+);/g)) {
    declarations[match[1]!] = normalize(match[2]!);
  }
  return declarations;
}

function plainDeclarations(
  source: string,
  selector: string,
): Record<string, string> {
  const start = source.indexOf(`${selector} {`);
  expect(start, `missing ${selector} block`).toBeGreaterThanOrEqual(0);
  const bodyStart = source.indexOf('{', start) + 1;
  const bodyEnd = source.indexOf('}', bodyStart);
  const declarations: Record<string, string> = {};

  for (const match of source
    .slice(bodyStart, bodyEnd)
    .matchAll(/(?<!-)\b([a-z-]+)\s*:\s*([^;]+);/g)) {
    declarations[match[1]!] = normalize(match[2]!);
  }
  return declarations;
}

function normalize(value: string): string {
  return value
    .trim()
    .replaceAll('"', '\'')
    .replace(/\s*,\s*/g, ',')
    .replace(/\s+/g, ' ')
    .replace(/#[\da-f]+/gi, (hex) => hex.toLowerCase());
}

function sourceFiles(directory: string): string[] {
  return readdirSync(directory, { withFileTypes: true }).flatMap((entry) => {
    const path = resolve(directory, entry.name);
    if (entry.isDirectory()) return sourceFiles(path);
    return /\.(?:css|ts|vue)$/.test(entry.name) ? [path] : [];
  });
}

function resolveValue(name: string, tokens: Record<string, string>): string {
  const value = tokens[name];
  expect(value, name).toBeDefined();
  return resolveColor(value!, tokens);
}

function resolveColor(
  value: string,
  tokens: Record<string, string>,
): string {
  const trimmed = value.trim();
  const varMatch = /^var\((--[\w-]+)\)$/u.exec(trimmed);
  if (varMatch) return resolveValue(varMatch[1]!, tokens);
  const mixMatch
    = /^color-mix\(in srgb,\s*(.+?) (\d+)%,\s*(.+?)\)$/u.exec(trimmed);
  if (mixMatch) {
    const fraction = Number(mixMatch[2]) / 100;
    const first = resolveColor(mixMatch[1]!, tokens);
    const second = resolveColor(mixMatch[3]!, tokens);
    return mixHex(first, second, fraction);
  }
  return trimmed;
}

function mixHex(first: string, second: string, fraction: number): string {
  const channel = (at: number): number => {
    const a = Number.parseInt(first.slice(1 + at, 3 + at), 16);
    const b = Number.parseInt(second.slice(1 + at, 3 + at), 16);
    return Math.round(a * fraction + b * (1 - fraction));
  };
  return `#${[0, 2, 4]
    .map((at) => channel(at).toString(16).padStart(2, '0'))
    .join('')}`;
}

function contrast(first: string, second: string): number {
  const [high, low] = [luminance(first), luminance(second)].sort(
    (a, b) => b - a,
  );
  return (high! + 0.05) / (low! + 0.05);
}

function luminance(hex: string): number {
  const match = /^#([0-9a-f]{6})$/iu.exec(hex.trim());
  if (match === null) throw new Error(`not a six-digit hex color: ${hex}`);
  const [r, g, b] = [0, 2, 4].map((at) => {
    const channel = Number.parseInt(match[1]!.slice(at, at + 2), 16) / 255;
    return channel <= 0.03928
      ? channel / 12.92
      : ((channel + 0.055) / 1.055) ** 2.4;
  });
  return 0.2126 * r! + 0.7152 * g! + 0.0722 * b!;
}
