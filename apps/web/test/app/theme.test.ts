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

const lightTokens = {
  '--radius': '6px',
  '--radius-sheet': '2px',
  '--radius-dialog': '8px',
  '--background': '#EDEFEB',
  '--foreground': '#171A18',
  '--card': '#FFFFFF',
  '--card-foreground': '#171A18',
  '--popover': '#FFFFFF',
  '--popover-foreground': '#171A18',
  '--primary': '#1F2A44',
  '--primary-foreground': '#FFFFFF',
  '--secondary': '#E3E6E1',
  '--secondary-foreground': '#171A18',
  '--muted': '#E3E6E1',
  '--muted-foreground': '#5F6763',
  '--accent': '#E3E6E1',
  '--accent-foreground': '#171A18',
  '--destructive': '#B42318',
  '--border': '#D8DDD9',
  '--input': '#D8DDD9',
  '--ring': '#1F2A44',
  '--seal': '#C8102E',
  '--seal-foreground': '#FFFFFF',
  '--shadow-paper':
    '0 1px 2px rgba(23,26,24,0.06),0 12px 32px rgba(23,26,24,0.1)',
} as const;

const darkTokens = {
  '--radius': '6px',
  '--radius-sheet': '2px',
  '--radius-dialog': '8px',
  '--background': '#121614',
  '--foreground': '#ECEFEC',
  '--card': '#1A1F1C',
  '--card-foreground': '#ECEFEC',
  '--popover': '#1A1F1C',
  '--popover-foreground': '#ECEFEC',
  '--primary': '#D7DEEE',
  '--primary-foreground': '#171A18',
  '--secondary': '#242A27',
  '--secondary-foreground': '#ECEFEC',
  '--muted': '#242A27',
  '--muted-foreground': '#9AA39E',
  '--accent': '#242A27',
  '--accent-foreground': '#ECEFEC',
  '--destructive': '#F0736A',
  '--border': 'rgba(255,255,255,0.12)',
  '--input': 'rgba(255,255,255,0.12)',
  '--ring': '#D7DEEE',
  '--seal': '#C8102E',
  '--seal-foreground': '#FFFFFF',
  '--shadow-paper': '0 1px 2px rgba(0,0,0,0.4),0 12px 32px rgba(0,0,0,0.5)',
} as const;

afterEach(() => {
  delete document.documentElement.dataset.theme;
});

describe('application theme', () => {
  it('defines the exact light and dark stamped-document tokens', () => {
    const css = readFileSync(themePath, 'utf8');

    expect(blockDeclarations(css, ':root')).toMatchObject(
      normalizedValues(lightTokens),
    );
    expect(blockDeclarations(css, 'html[data-theme="dark"]')).toMatchObject(
      normalizedValues(darkTokens),
    );
    expect(normalize(lightTokens['--muted'])).toBe(
      normalize(lightTokens['--secondary']),
    );
    expect(normalize(darkTokens['--muted'])).toBe(
      normalize(darkTokens['--secondary']),
    );
    expect(css).not.toMatch(/--positive(?:-foreground)?\s*:/);
    expect(css).not.toMatch(/--chart-/);
  });

  it('maps the chrome font, seal colors, radii, shadow, and type scale', () => {
    const css = readFileSync(themePath, 'utf8');
    const theme = blockDeclarations(css, '@theme inline');

    expect(theme['--font-sans']).toBe(
      normalize('\'Be Vietnam Pro\', \'Inter\', system-ui, sans-serif'),
    );
    expect(theme['--color-seal']).toBe('var(--seal)');
    expect(theme['--color-seal-foreground']).toBe('var(--seal-foreground)');
    expect(theme['--text-xs']).toBe('0.75rem');
    expect(theme['--text-sm']).toBe('0.8125rem');
    expect(theme['--text-base']).toBe('0.875rem');
    expect(theme['--text-md']).toBe('1rem');
    expect(theme['--text-lg']).toBe('1.25rem');
    expect(theme['--text-xl']).toBe('1.5rem');
    expect(theme['--text-2xl']).toBe('2rem');
    expect(theme['--text-3xl']).toBe('2.75rem');
    for (const step of ['xs', 'sm', 'base', 'md']) {
      expect(theme[`--text-${step}--line-height`]).toBe('1.5');
    }
    for (const step of ['lg', 'xl', '2xl', '3xl']) {
      expect(theme[`--text-${step}--line-height`]).toBe('1.2');
    }
    expect(theme['--radius-sm']).toBe('calc(var(--radius) - 4px)');
    expect(theme['--radius-md']).toBe('calc(var(--radius) - 2px)');
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
