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

// Application chrome tokens (ADR 0020). Resume
// renderer tokens are covered separately below; they must never move with
// this palette.
const lightTokens = {
  '--radius': '10px',
  '--radius-sheet': '2px',
  '--radius-dialog': '14px',
  '--radius-feature': '20px',
  '--background': '#f9f8f5',
  '--foreground': '#101b3f',
  '--card': '#ffffff',
  '--card-foreground': '#101b3f',
  '--popover': '#ffffff',
  '--popover-foreground': '#101b3f',
  '--primary': '#26409c',
  '--primary-foreground': '#ffffff',
  '--secondary': '#eceef6',
  '--secondary-foreground': '#101b3f',
  '--muted': '#efede6',
  '--muted-foreground': '#5c6178',
  '--accent': '#eceef6',
  '--accent-foreground': '#101b3f',
  '--destructive': '#b54708',
  '--border': '#e5e1d6',
  '--input': '#7d8398',
  '--ring': '#26409c',
  '--seal': '#cc2649',
  '--seal-foreground': '#ffffff',
  '--seal-text': '#cc2649',
  '--link': '#23399a',
  '--brand-blue': 'var(--primary)',
  '--brand-deep-blue': 'var(--link)',
  '--brand-indigo': '#4a4dbf',
  '--brand-jade': '#0f7c6e',
  '--brand-ochre': '#8f6f0d',
  '--surface-blue': '#eceef7',
  '--surface-indigo': '#efeef8',
  '--surface-sand': '#f6eedf',
  '--surface-destructive':
    'color-mix(in srgb, var(--destructive) 7%, var(--card))',
  '--editor-canvas': '#f1efe9',
  '--space-module': '8px',
  '--space-dialog-title': '6px',
  '--space-field': '16px',
  '--space-dialog': '24px',
  '--space-section': '80px',
  '--space-section-lg': '112px',
  '--primary-hover': '#1e3483',
  '--shadow-primary':
    '0 1px 2px rgb(16 27 63 / 0.08), 0 4px 12px rgb(38 64 156 / 0.2)',
  '--paper': '#ffffff',
  '--paper-ink': '#171a18',
  '--paper-muted': '#5f6763',
  '--paper-hover': '#f0f2f1',
  '--shadow-paper':
    '0 1px 2px rgba(23,26,24,0.06),0 12px 32px rgba(23,26,24,0.1)',
  '--gradient-brand-strong': 'linear-gradient(90deg,#1e3483,#3d40ad)',
  '--shadow-cta': '0 8px 24px rgb(38 64 156 / 0.24)',
} as const;

// The paper tokens are defined in :root only (section 1); they never enter
// the dark block, so the sheet keeps one white value in both themes.
const paperOnlyTokens = [
  '--paper',
  '--paper-ink',
  '--paper-muted',
  '--paper-hover',
] as const;

const darkTokens = {
  '--radius': '10px',
  '--radius-sheet': '2px',
  '--radius-dialog': '14px',
  '--radius-feature': '20px',
  '--background': '#0c1020',
  '--foreground': '#f3f1ec',
  '--card': '#141a2e',
  '--card-foreground': '#f3f1ec',
  '--popover': '#1a2138',
  '--popover-foreground': '#f3f1ec',
  '--primary': '#8fa6f0',
  '--primary-foreground': '#0c1020',
  '--secondary': '#1a2138',
  '--secondary-foreground': '#f3f1ec',
  '--muted': '#1a2138',
  '--muted-foreground': '#a5abbf',
  '--accent': '#212a45',
  '--accent-foreground': '#f3f1ec',
  '--destructive': '#fd8a4b',
  '--border': 'rgba(230,225,210,0.13)',
  '--input': '#6a7390',
  '--ring': '#8fa6f0',
  '--seal': '#cc2649',
  '--seal-foreground': '#ffffff',
  '--seal-text': '#ff6b8a',
  '--link': '#a3b6f5',
  '--brand-blue': 'var(--primary)',
  '--brand-deep-blue': 'var(--link)',
  '--brand-indigo': '#9a9cff',
  '--brand-jade': '#5fcfbb',
  '--brand-ochre': '#e6c35f',
  '--surface-blue': '#151d36',
  '--surface-indigo': '#1a1b3d',
  '--surface-sand': '#231d16',
  '--surface-destructive':
    'color-mix(in srgb, var(--destructive) 12%, var(--card))',
  '--editor-canvas': '#0c1020',
  '--primary-hover': '#a3b6f5',
  '--shadow-primary': '0 1px 2px rgb(0 0 0 / 0.4)',
  '--shadow-paper': '0 1px 2px rgba(0,0,0,0.4),0 12px 32px rgba(0,0,0,0.5)',
  '--gradient-brand-strong': 'linear-gradient(90deg,#a3b6f5,#b0b2ff)',
  '--shadow-cta': '0 8px 24px rgb(143 166 240 / 0.18)',
} as const;

afterEach(() => {
  delete document.documentElement.dataset.theme;
});

describe('application theme', () => {
  it('defines the exact light and dark Aurora tokens', () => {
    const css = readFileSync(themePath, 'utf8');

    expect(blockDeclarations(css, ':root')).toMatchObject(
      normalizedValues(lightTokens),
    );
    const dark = blockDeclarations(css, 'html[data-theme="dark"]');
    expect(dark).toMatchObject(normalizedValues(darkTokens));
    for (const token of paperOnlyTokens) {
      expect(dark[token]).toBeUndefined();
    }
    expect(css).not.toMatch(/--positive(?:-foreground)?\s*:/);
    expect(css).not.toMatch(/--chart-/);
  });

  it('defines a three-stop hero glow in both themes', () => {
    const css = readFileSync(themePath, 'utf8');
    const light = blockDeclarations(css, ':root')['--gradient-hero-glow'];
    const dark = blockDeclarations(css, 'html[data-theme="dark"]')[
      '--gradient-hero-glow'
    ];
    for (const value of [light, dark]) {
      expect(value).toBeDefined();
      expect(value!.match(/radial-gradient/gu)).toHaveLength(3);
      expect(value).toContain('transparent 70%');
    }
    expect(light).toContain('rgb(15 124 110 / 0.16)');
    expect(light).toContain('rgb(38 64 156 / 0.18)');
    expect(light).toContain('rgb(143 111 13 / 0.16)');
    expect(dark).toContain('rgb(95 207 187 / 0.14)');
    expect(dark).toContain('rgb(143 166 240 / 0.22)');
    expect(dark).toContain('rgb(230 195 95 / 0.12)');
  });

  it('maps the chrome font, seal colors, radii, shadow, and type scale', () => {
    const css = readFileSync(themePath, 'utf8');
    const theme = blockDeclarations(css, '@theme inline');

    expect(theme['--font-sans']).toBe(
      normalize('\'Be Vietnam Pro\', \'Inter\', system-ui, sans-serif'),
    );
    expect(theme['--color-seal']).toBe('var(--seal)');
    expect(theme['--color-seal-foreground']).toBe('var(--seal-foreground)');
    expect(theme['--color-seal-text']).toBe('var(--seal-text)');
    expect(theme['--color-link']).toBe('var(--link)');
    for (const brand of [
      'blue', 'deep-blue', 'indigo', 'jade', 'ochre',
    ]) {
      expect(theme[`--color-brand-${brand}`]).toBe(`var(--brand-${brand})`);
    }
    // Pink, orange, cyan, and purple are not brand colors (ADR 0020).
    for (const retired of ['pink', 'orange', 'cyan', 'purple']) {
      expect(theme[`--color-brand-${retired}`]).toBeUndefined();
    }
    expect(theme['--color-surface-pink']).toBeUndefined();
    for (const surface of ['blue', 'indigo', 'sand', 'destructive']) {
      expect(theme[`--color-surface-${surface}`]).toBe(
        `var(--surface-${surface})`,
      );
    }
    expect(theme['--color-editor-canvas']).toBe('var(--editor-canvas)');
    expect(theme['--color-primary-hover']).toBe('var(--primary-hover)');
    expect(theme['--color-paper']).toBe('var(--paper)');
    expect(theme['--color-paper-ink']).toBe('var(--paper-ink)');
    expect(theme['--color-paper-muted']).toBe('var(--paper-muted)');
    expect(theme['--color-paper-hover']).toBe('var(--paper-hover)');
    expect(theme['--text-xs']).toBe('0.75rem');
    expect(theme['--text-sm']).toBe('0.875rem');
    expect(theme['--text-base']).toBe('0.9375rem');
    expect(theme['--text-md']).toBe('1rem');
    expect(theme['--text-lg']).toBe('1.25rem');
    expect(theme['--text-xl']).toBe('1.5rem');
    expect(theme['--text-2xl']).toBe('2rem');
    expect(theme['--text-3xl']).toBe('2.5rem');
    expect(theme['--text-4xl']).toBe('3rem');
    expect(theme['--text-6xl']).toBe('3.75rem');
    expect(theme['--text-6xl--line-height']).toBe('1.2');
    // The gallery page uses the Tailwind default text-5xl; the homepage
    // hero does not need it overridden (DESIGN.md; ADR 0020).
    expect(theme['--text-5xl']).toBeUndefined();
    for (const step of ['xs', 'sm', 'base', 'md']) {
      expect(theme[`--text-${step}--line-height`]).toBe('1.5');
    }
    // Heading steps leave room for stacked Vietnamese diacritics.
    for (const step of ['lg', 'xl', '2xl']) {
      expect(theme[`--text-${step}--line-height`]).toBe('1.3');
    }
    for (const step of ['3xl', '4xl']) {
      expect(theme[`--text-${step}--line-height`]).toBe('1.25');
    }
    expect(theme['--radius-sm']).toBe('calc(var(--radius) - 4px)');
    expect(theme['--radius-md']).toBe('var(--radius)');
    expect(theme['--radius-lg']).toBe('var(--radius)');
    expect(theme['--radius-xl']).toBe('calc(var(--radius) + 4px)');
    expect(css).not.toContain('--shadow-paper: var(--shadow-paper)');
    expect(css).not.toContain('--radius-sheet: var(--radius-sheet)');
    expect(css).not.toContain('--radius-dialog: var(--radius-dialog)');
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

      // The logo carries the seal (ADR 0020): its mark and the dot of .vn.
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
      ).toContain('bg-seal text-seal-foreground hover:bg-seal/90');
    },
  );

  it('keeps chrome text and marks at WCAG AA on every ground', () => {
    const css = readFileSync(themePath, 'utf8');
    for (const selector of [':root', 'html[data-theme="dark"]']) {
      const tokens = { ...blockDeclarations(css, ':root') };
      Object.assign(tokens, blockDeclarations(css, selector));
      const grounds = [
        '--background', '--card', '--muted', '--surface-blue',
        '--surface-indigo', '--surface-sand', '--secondary', '--accent',
        '--popover', '--editor-canvas', '--surface-destructive',
      ];
      // Body text needs 4.5:1; icons, dots, and input borders need 3:1.
      const text = [
        '--foreground', '--muted-foreground', '--link', '--seal-text',
        '--destructive',
      ];
      const marks = [
        '--primary', '--brand-indigo', '--brand-jade', '--brand-ochre',
      ];
      for (const ground of grounds) {
        const groundHex = resolveColor(tokens[ground]!, tokens);
        for (const fg of text) {
          expect(
            contrast(tokens[fg]!, groundHex),
            `${selector} ${fg} on ${ground}`,
          ).toBeGreaterThanOrEqual(4.5);
        }
        for (const fg of marks) {
          expect(
            contrast(tokens[fg]!, groundHex),
            `${selector} ${fg} on ${ground}`,
          ).toBeGreaterThanOrEqual(3);
        }
      }
      for (const ground of ['--background', '--card']) {
        expect(
          contrast(tokens['--input']!, tokens[ground]!),
          `${selector} --input on ${ground}`,
        ).toBeGreaterThanOrEqual(3);
      }
      for (const fill of ['--primary', '--primary-hover']) {
        expect(
          contrast(tokens['--primary-foreground']!, tokens[fill]!),
          `${selector} --primary-foreground on ${fill}`,
        ).toBeGreaterThanOrEqual(4.5);
      }
      expect(
        contrast(tokens['--seal-foreground']!, tokens['--seal']!),
      ).toBeGreaterThanOrEqual(4.5);
    }
  });

  it('scopes .paper-surface to the paper tokens, not the app palette', () => {
    const css = readFileSync(themePath, 'utf8');
    const scope = blockDeclarations(css, '.paper-surface');
    const plain = plainDeclarations(css, '.paper-surface');

    expect(scope['--foreground']).toBe('var(--paper-ink)');
    expect(scope['--muted-foreground']).toBe('var(--paper-muted)');
    expect(scope['--accent']).toBe('var(--paper-hover)');
    expect(scope['--accent-foreground']).toBe('var(--paper-ink)');
    expect(scope['--ring']).toBe('#26409c');
    expect(scope['--link']).toBe('#23399a');
    expect(plain['background-color']).toBe('var(--paper)');
    expect(plain['color']).toBe('var(--paper-ink)');
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
        );
      })
      .map((path) => path.slice(webRoot.length + 1));

    expect(consumers).toEqual([]);
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

function blockDeclarations(
  css: string,
  selector: string,
): Record<string, string> {
  const start = css.indexOf(`${selector} {`);
  expect(start, `missing ${selector} block`).toBeGreaterThanOrEqual(0);
  const bodyStart = css.indexOf('{', start) + 1;
  const bodyEnd = css.indexOf('}', bodyStart);
  const declarations: Record<string, string> = {};

  for (const match of css
    .slice(bodyStart, bodyEnd)
    .matchAll(/(--[\w-]+)\s*:\s*([^;]+);/g)) {
    declarations[match[1]!] = normalize(match[2]!);
  }
  return declarations;
}

function plainDeclarations(
  css: string,
  selector: string,
): Record<string, string> {
  const start = css.indexOf(`${selector} {`);
  expect(start, `missing ${selector} block`).toBeGreaterThanOrEqual(0);
  const bodyStart = css.indexOf('{', start) + 1;
  const bodyEnd = css.indexOf('}', bodyStart);
  const declarations: Record<string, string> = {};

  for (const match of css
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
    .replace(/#[\da-f]+/gi, (hex) => hex.toLowerCase());
}

function normalizedValues(
  values: Readonly<Record<string, string>>,
): Record<string, string> {
  return Object.fromEntries(
    Object.entries(values).map(([name, value]) => [name, normalize(value)]),
  );
}

function sourceFiles(directory: string): string[] {
  return readdirSync(directory, { withFileTypes: true }).flatMap((entry) => {
    const path = resolve(directory, entry.name);
    if (entry.isDirectory()) return sourceFiles(path);
    return /\.(?:css|ts|vue)$/.test(entry.name) ? [path] : [];
  });
}

// Resolves a token value to a six-digit hex color, following a var()
// reference or computing a color-mix() the way the browser would, so
// grounds built from either (--surface-destructive) can be checked for
// contrast like any other ground.
function resolveColor(
  value: string,
  tokens: Record<string, string>,
): string {
  const trimmed = value.trim();
  const varMatch = /^var\((--[\w-]+)\)$/u.exec(trimmed);
  if (varMatch) return resolveColor(tokens[varMatch[1]!]!, tokens);
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
