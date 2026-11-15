import { readFileSync } from 'node:fs';
import { describe, expect, it } from 'vitest';

const tokensCss = readFileSync('app/landing/styles/tokens.css', 'utf8');
const homeCss = readFileSync('app/landing/styles/home.css', 'utf8');
const LIGHT = 'html[data-page="home"]';
const DARK = 'html[data-page="home"][data-theme="dark"]';

const SPEC = {
  light: {
    '--background': '#ffffff',
    '--foreground': '#1a1650',
    '--muted-foreground': '#55517d',
    '--brand': '#de1b5a',
    '--primary': '#de1b5a',
    '--seal': '#de1b5a',
    '--brand-foreground': '#ffffff',
    '--brand-hover': '#c7134d',
    '--brand-text': '#c7134d',
    '--seal-text': '#c7134d',
    '--surface-mist': '#f1effa',
    '--surface-blush': '#ffe4ec',
    '--surface-sun': '#fff2c2',
    '--panel': '#1a1650',
    '--footer': '#1a1650',
    '--panel-foreground': '#ffffff',
    '--panel-muted': '#d9d6f2',
    '--card': '#ffffff',
    '--border': '#e3e0f2',
    '--hairline': 'rgba(26,22,80,0.14)',
    '--control': '#7f7aae',
    '--coin': '#ffd23f',
    '--coin-edge': '#e9b416',
    '--coin-ink': '#1a1650',
    '--ring': '#1a1650',
    '--ring-inverse': '#ffffff',
    '--logo-mark': '#de1b5a',
    '--logo-mark-inverse': '#ff7aa2',
  },
  dark: {
    '--background': '#0f0d26',
    '--foreground': '#f3f1ff',
    '--muted-foreground': '#b7b2db',
    '--brand': '#de1b5a',
    '--primary': '#de1b5a',
    '--seal': '#de1b5a',
    '--brand-foreground': '#ffffff',
    '--brand-hover': '#c7134d',
    '--brand-text': '#ff7aa2',
    '--seal-text': '#ff7aa2',
    '--surface-mist': '#191640',
    '--surface-blush': '#3b1431',
    '--surface-sun': '#332a0e',
    '--panel': '#322c8f',
    '--footer': '#191640',
    '--panel-foreground': '#ffffff',
    '--panel-muted': '#d9d6f2',
    '--card': '#25225a',
    '--border': '#34315c',
    '--hairline': 'rgba(243,241,255,0.16)',
    '--control': '#8580b8',
    '--coin': '#ffd23f',
    '--coin-edge': '#e9b416',
    '--coin-ink': '#1a1650',
    '--ring': '#c9c3ff',
    '--ring-inverse': '#ffffff',
    '--logo-mark': '#ff7aa2',
    '--logo-mark-inverse': '#ff7aa2',
  },
} as const;

type Pair = readonly [foreground: string, ground: string, expected: number];

const CONTRAST: Record<'light' | 'dark', {
  readonly text: readonly Pair[];
  readonly marks: readonly Pair[];
}> = {
  light: {
    text: [
      ['--foreground', '--background', 16.5],
      ['--foreground', '--surface-mist', 14.5],
      ['--foreground', '--surface-blush', 13.8],
      ['--foreground', '--surface-sun', 14.7],
      ['--foreground', '--card', 16.5],
      ['--muted-foreground', '--background', 7.3],
      ['--muted-foreground', '--surface-mist', 6.5],
      ['--muted-foreground', '--surface-blush', 6.1],
      ['--muted-foreground', '--surface-sun', 6.6],
      ['--muted-foreground', '--card', 7.3],
      ['--brand-foreground', '--brand', 4.76],
      ['--brand-foreground', '--brand-hover', 5.79],
      ['--brand-text', '--background', 5.79],
      ['--brand-text', '--card', 5.79],
      ['--brand-text', '--surface-mist', 5.09],
      ['--panel-foreground', '--panel', 16.5],
      ['--panel-muted', '--panel', 11.7],
      ['--coin-ink', '--coin', 11.4],
    ],
    marks: [
      ['--control', '--card', 3.96],
      ['--control', '--surface-mist', 3.49],
      ['--control', '--surface-sun', 3.54],
      ['--ring', '--background', 16.5],
      ['--ring', '--surface-mist', 14.5],
      ['--ring', '--surface-blush', 13.8],
      ['--brand', '--background', 4.76],
      ['--brand', '--card', 4.76],
    ],
  },
  dark: {
    text: [
      ['--foreground', '--background', 17.0],
      ['--foreground', '--surface-mist', 15.3],
      ['--foreground', '--surface-blush', 14.2],
      ['--foreground', '--surface-sun', 12.7],
      ['--foreground', '--card', 13.0],
      ['--muted-foreground', '--background', 9.4],
      ['--muted-foreground', '--surface-mist', 8.5],
      ['--muted-foreground', '--surface-blush', 7.8],
      ['--muted-foreground', '--surface-sun', 7.0],
      ['--muted-foreground', '--card', 7.2],
      ['--brand-foreground', '--brand', 4.76],
      ['--brand-foreground', '--brand-hover', 5.79],
      ['--brand-text', '--background', 7.74],
      ['--brand-text', '--card', 5.88],
      ['--brand-text', '--surface-mist', 6.96],
      ['--panel-foreground', '--panel', 11.1],
      ['--panel-muted', '--panel', 7.9],
      ['--coin-ink', '--coin', 11.4],
    ],
    marks: [
      ['--control', '--card', 3.97],
      ['--control', '--surface-mist', 4.70],
      ['--control', '--surface-sun', 3.91],
      ['--ring', '--background', 11.5],
      ['--ring', '--surface-mist', 10.4],
      ['--ring', '--surface-blush', 9.6],
      ['--brand', '--background', 3.99],
      ['--brand', '--card', 3.03],
    ],
  },
};

describe('home tokens', () => {
  it('scopes every token and override to the home root element', () => {
    for (const selector of selectors(tokensCss)) {
      expect(selector.startsWith(LIGHT), selector).toBe(true);
    }
    for (const selector of selectors(homeCss)) {
      expect(selector, selector).toMatch(/^\.home(?:[-\s.:,]|$)/u);
    }
  });

  for (const theme of ['light', 'dark'] as const) {
    it(`matches the spec color table in ${theme}`, () => {
      const tokens = themeTokens(theme);
      for (const [name, value] of Object.entries(SPEC[theme])) {
        expect(resolve(name, tokens), name).toBe(value);
      }
    });

    it(`meets the spec contrast in ${theme}`, () => {
      const tokens = themeTokens(theme);
      for (const [minimum, pairs] of [
        [4.5, CONTRAST[theme].text],
        [3, CONTRAST[theme].marks],
      ] as const) {
        for (const [foreground, ground, expected] of pairs) {
          const ratio = contrast(
            resolve(foreground, tokens), resolve(ground, tokens),
          );
          const label = `${foreground} on ${ground}`;
          expect(ratio, label).toBeGreaterThanOrEqual(minimum);
          expect(Math.abs(ratio - expected), label).toBeLessThan(
            Number.isInteger(expected * 10) ? 0.06 : 0.006,
          );
        }
      }
    });

    it(`keeps seal words readable on the ticket fill in ${theme}`, () => {
      const tokens = themeTokens(theme);
      const ink = resolve('--seal-text', tokens);
      const fill = mix(ink, resolve('--card', tokens), 0.07);
      expect(contrast(ink, fill)).toBeGreaterThanOrEqual(4.5);
    });
  }

  it('keeps the hero seal and sheet light in both themes', () => {
    for (const theme of ['light', 'dark'] as const) {
      const tokens = themeTokens(theme);
      const ink = resolve('--paper-seal-ink', tokens);
      const ground = resolve('--paper-seal-ground', tokens);
      expect([ink, ground]).toEqual(['#c7134d', '#ffffff']);
      expect(contrast(ink, mix(ink, ground, 0.07)))
        .toBeGreaterThanOrEqual(4.5);
    }
  });

  it('defines the spec radii, shadows and chrome typeface', () => {
    const light = declarations(tokensCss, LIGHT);
    const dark = declarations(tokensCss, DARK);
    expect(light['--shadow-paper']).toBe(
      '0 36px 56px -28px rgba(0,0,0,0.5),0 2px 6px rgba(0,0,0,0.16)',
    );
    expect(dark['--shadow-paper']).toBeUndefined();
    expect(light['--shadow-float'])
      .toBe('0 16px 24px -14px rgba(26,22,80,0.55)');
    expect(dark['--shadow-float']).toBe('0 16px 24px -14px rgba(0,0,0,0.7)');
    expect(light['--shadow-card'])
      .toBe('0 32px 48px -32px rgba(26,22,80,0.45)');
    expect(dark['--shadow-card']).toBe('0 32px 48px -32px rgba(0,0,0,0.75)');
    expect(light['--gradient-aurora']).toBe('none');
    expect(light['--font-chrome']).toBe(
      '\'Plus Jakarta Sans\',\'Be Vietnam Pro\',system-ui,sans-serif',
    );
    expect(light['--font-sans']).toBe('var(--font-chrome)');
    expect(light['--font-paper']).toBe(
      '\'Be Vietnam Pro\',\'Inter\',system-ui,sans-serif',
    );
    expect([light['--radius-panel'], light['--radius-card']])
      .toEqual(['28px', '22px']);
    expect(mediaTokens('48rem')).toEqual({
      '--radius-panel': '36px', '--radius-card': '26px',
    });
    expect(mediaTokens('64rem')).toEqual({
      '--radius-panel': '44px', '--radius-card': '28px',
    });
  });

  it('gives the dark header the page ground and a hairline', () => {
    const header = `${DARK} header[data-testid="app-shell"]`;
    const start = tokensCss.indexOf(`${header} {`);
    expect(start).toBeGreaterThanOrEqual(0);
    const body = tokensCss.slice(
      tokensCss.indexOf('{', start) + 1, tokensCss.indexOf('}', start),
    );
    expect(body.replace(/\s+/gu, ' ').trim()).toBe(
      'border-bottom-color: var(--hairline); '
      + 'background-color: var(--background);',
    );
    expect(tokensCss.match(/header\[data-testid="app-shell"\]/gu))
      .toHaveLength(1);
  });

  it('sets the seal in the document face, not the chrome face', () => {
    expect(homeCss).toMatch(
      /\.home \[data-app-seal\] \{\s*font-family: var\(--font-paper\);/u,
    );
  });
});

function selectors(source: string): string[] {
  const found: string[] = [];
  let boundary = 0;
  for (let index = 0; index < source.length; index += 1) {
    if (source[index] === '{') {
      const prelude = source.slice(boundary, index).trim();
      if (prelude !== '' && !prelude.startsWith('@')) {
        found.push(...prelude.split(',').map((part) => part.trim()));
      }
      boundary = index + 1;
    } else if (source[index] === '}') {
      boundary = index + 1;
    }
  }
  return found;
}

function declarations(css: string, selector: string): Record<string, string> {
  const start = css.indexOf(`${selector} {`);
  expect(start, `missing ${selector}`).toBeGreaterThanOrEqual(0);
  const bodyStart = css.indexOf('{', start) + 1;
  const body = css.slice(bodyStart, css.indexOf('}', bodyStart));
  const out: Record<string, string> = {};
  for (const match of body.matchAll(/(--[\w-]+)\s*:\s*([^;]+);/gu)) {
    out[match[1]!] = match[2]!.trim().replaceAll('"', '\'')
      .replace(/\s*,\s*/gu, ',').replace(/\s+/gu, ' ');
  }
  return out;
}

function mediaTokens(width: string): Record<string, string> {
  const start = tokensCss.indexOf(`@media (min-width: ${width}) {`);
  expect(start, width).toBeGreaterThanOrEqual(0);
  return declarations(tokensCss.slice(start), `  ${LIGHT}`);
}

function themeTokens(theme: 'light' | 'dark'): Record<string, string> {
  const light = declarations(tokensCss, LIGHT);
  return theme === 'light'
    ? light
    : { ...light, ...declarations(tokensCss, DARK) };
}

function resolve(name: string, tokens: Record<string, string>): string {
  const value = tokens[name];
  expect(value, name).toBeDefined();
  const reference = /^var\((--[\w-]+)\)$/u.exec(value!);
  return reference ? resolve(reference[1]!, tokens) : value!;
}

function mix(first: string, second: string, fraction: number): string {
  return `#${[1, 3, 5].map((at) => {
    const a = Number.parseInt(first.slice(at, at + 2), 16);
    const b = Number.parseInt(second.slice(at, at + 2), 16);
    return Math.round(a * fraction + b * (1 - fraction))
      .toString(16).padStart(2, '0');
  }).join('')}`;
}

function contrast(first: string, second: string): number {
  const [high, low] = [luminance(first), luminance(second)]
    .sort((a, b) => b - a);
  return (high! + 0.05) / (low! + 0.05);
}

function luminance(hex: string): number {
  const match = /^#([0-9a-f]{6})$/u.exec(hex);
  if (match === null) throw new Error(`not a six-digit hex color: ${hex}`);
  const [r, g, b] = [0, 2, 4].map((at) => {
    const channel = Number.parseInt(match[1]!.slice(at, at + 2), 16) / 255;
    return channel <= 0.03928
      ? channel / 12.92
      : ((channel + 0.055) / 1.055) ** 2.4;
  });
  return 0.2126 * r! + 0.7152 * g! + 0.0722 * b!;
}
