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

// Aurora application chrome tokens (ADR 0050). Resume renderer tokens are
// covered separately below; they must never move with this palette.
const lightTokens = {
  '--radius': '10px',
  '--radius-sheet': '2px',
  '--radius-dialog': '14px',
  '--radius-feature': '20px',
  '--background': '#f5f8ff',
  '--foreground': '#101b3f',
  '--card': '#ffffff',
  '--card-foreground': '#101b3f',
  '--popover': '#ffffff',
  '--popover-foreground': '#101b3f',
  '--primary': '#1a5ceb',
  '--primary-foreground': '#ffffff',
  '--secondary': '#eaf2ff',
  '--secondary-foreground': '#101b3f',
  '--muted': '#edf1fa',
  '--muted-foreground': '#56648c',
  '--accent': '#eaf2ff',
  '--accent-foreground': '#101b3f',
  '--destructive': '#b42318',
  '--border': '#dce5f5',
  '--input': '#7886ae',
  '--ring': '#1a5ceb',
  '--seal': '#cc2649',
  '--seal-foreground': '#ffffff',
  '--link': '#123edb',
  '--brand-blue': '#246bfd',
  '--brand-deep-blue': '#123edb',
  '--brand-indigo': '#6254ff',
  '--brand-cyan': '#35c8f5',
  '--brand-purple': '#a855f7',
  '--brand-pink': '#f55db1',
  '--brand-orange': '#ff9c47',
  '--surface-blue': '#eaf2ff',
  '--surface-indigo': '#f0eeff',
  '--surface-pink': '#fff0fa',
  '--editor-canvas': '#eef3fc',
  '--primary-hover': '#1550d4',
  '--shadow-primary':
    '0 1px 2px rgb(16 27 63 / 0.08), 0 4px 12px rgb(26 92 235 / 0.2)',
  '--paper': '#ffffff',
  '--paper-ink': '#171a18',
  '--paper-muted': '#5f6763',
  '--paper-hover': '#f0f2f1',
  '--shadow-paper':
    '0 1px 2px rgba(23,26,24,0.06),0 12px 32px rgba(23,26,24,0.1)',
  '--gradient-brand-strong': 'linear-gradient(90deg,#1a5ceb,#5144f0)',
  '--shadow-cta': '0 8px 24px rgb(36 107 253 / 0.28)',
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
  '--background': '#071126',
  '--foreground': '#f4f7ff',
  '--card': '#0d1935',
  '--card-foreground': '#f4f7ff',
  '--popover': '#132244',
  '--popover-foreground': '#f4f7ff',
  '--primary': '#72a0ff',
  '--primary-foreground': '#071126',
  '--secondary': '#132244',
  '--secondary-foreground': '#f4f7ff',
  '--muted': '#132244',
  '--muted-foreground': '#9eacca',
  '--accent': '#1a2b52',
  '--accent-foreground': '#f4f7ff',
  '--destructive': '#f0736a',
  '--border': 'rgba(180,200,255,0.16)',
  '--input': '#5a6a95',
  '--ring': '#72a0ff',
  '--seal': '#cc2649',
  '--seal-foreground': '#ffffff',
  '--link': '#8fb3ff',
  '--brand-blue': '#72a0ff',
  '--brand-deep-blue': '#4c7dff',
  '--brand-indigo': '#8b80ff',
  '--brand-cyan': '#54d6ff',
  '--brand-purple': '#c08bff',
  '--brand-pink': '#ff7cc4',
  '--brand-orange': '#ffb067',
  '--surface-blue': '#10224a',
  '--surface-indigo': '#1a1a4a',
  '--surface-pink': '#2a1533',
  '--editor-canvas': '#071126',
  '--primary-hover': '#8fb3ff',
  '--shadow-primary': '0 1px 2px rgb(0 0 0 / 0.4)',
  '--shadow-paper': '0 1px 2px rgba(0,0,0,0.4),0 12px 32px rgba(0,0,0,0.5)',
  '--gradient-brand-strong': 'linear-gradient(90deg,#8fb3ff,#a39bff)',
  '--shadow-cta': '0 8px 24px rgb(114 160 255 / 0.22)',
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
    expect(light).toContain('rgb(53 200 245 / 0.3)');
    expect(light).toContain('rgb(36 107 253 / 0.24)');
    expect(light).toContain('rgb(168 85 247 / 0.18)');
    expect(dark).toContain('rgb(84 214 255 / 0.22)');
    expect(dark).toContain('rgb(114 160 255 / 0.3)');
    expect(dark).toContain('rgb(192 139 255 / 0.22)');
  });

  it('maps the chrome font, seal colors, radii, shadow, and type scale', () => {
    const css = readFileSync(themePath, 'utf8');
    const theme = blockDeclarations(css, '@theme inline');

    expect(theme['--font-sans']).toBe(
      normalize('\'Be Vietnam Pro\', \'Inter\', system-ui, sans-serif'),
    );
    expect(theme['--color-seal']).toBe('var(--seal)');
    expect(theme['--color-seal-foreground']).toBe('var(--seal-foreground)');
    expect(theme['--color-link']).toBe('var(--link)');
    for (const brand of [
      'blue', 'deep-blue', 'indigo', 'cyan', 'purple', 'pink', 'orange',
    ]) {
      expect(theme[`--color-brand-${brand}`]).toBe(`var(--brand-${brand})`);
    }
    for (const surface of ['blue', 'indigo', 'pink']) {
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
    expect(theme['--text-sm']).toBe('0.8125rem');
    expect(theme['--text-base']).toBe('0.875rem');
    expect(theme['--text-md']).toBe('1rem');
    expect(theme['--text-lg']).toBe('1.25rem');
    expect(theme['--text-xl']).toBe('1.5rem');
    expect(theme['--text-2xl']).toBe('2rem');
    expect(theme['--text-3xl']).toBe('2.75rem');
    expect(theme['--text-4xl']).toBe('2.5rem');
    expect(theme['--text-4xl--line-height']).toBe('1.15');
    expect(theme['--text-6xl']).toBe('3.75rem');
    expect(theme['--text-6xl--line-height']).toBe('1.1');
    // The gallery page uses the Tailwind default text-5xl; the homepage
    // hero does not need it overridden (DESIGN.md; ADR 0050).
    expect(theme['--text-5xl']).toBeUndefined();
    for (const step of ['xs', 'sm', 'base', 'md']) {
      expect(theme[`--text-${step}--line-height`]).toBe('1.5');
    }
    for (const step of ['lg', 'xl', '2xl', '3xl']) {
      expect(theme[`--text-${step}--line-height`]).toBe('1.2');
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

      expect(consumers).toEqual([
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

  it('scopes .paper-surface to the paper tokens, not the app palette', () => {
    const css = readFileSync(themePath, 'utf8');
    const scope = blockDeclarations(css, '.paper-surface');
    const plain = plainDeclarations(css, '.paper-surface');

    expect(scope['--foreground']).toBe('var(--paper-ink)');
    expect(scope['--muted-foreground']).toBe('var(--paper-muted)');
    expect(scope['--accent']).toBe('var(--paper-hover)');
    expect(scope['--accent-foreground']).toBe('var(--paper-ink)');
    expect(scope['--ring']).toBe('#1a5ceb');
    expect(scope['--link']).toBe('#123edb');
    expect(plain['background-color']).toBe('var(--paper)');
    expect(plain['color']).toBe('var(--paper-ink)');
  });

  it('has no retained utility consumer of removed palette tokens', () => {
    const consumers = sourceFiles(resolve(webRoot, 'app'))
      .filter((path) => path !== themePath)
      .filter((path) => {
        const source = readFileSync(path, 'utf8');
        return /\b(?:bg|text|border)-(?:positive(?:\/\d+)?|chart-[1-5])\b/
          .test(source);
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
