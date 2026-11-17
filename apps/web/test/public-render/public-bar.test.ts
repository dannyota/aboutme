// @vitest-environment node

import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { describe, expect, it } from 'vitest';

import {
  PROJECT_PAGE_STYLE,
} from '../../server/workers/public-render/project-style';
import { lastIndex, matching, parseCss, style } from './css-rules';

type Palette = Readonly<Record<string, string>>;

const LIGHT: Palette = {
  '--toolbar-ground': '#ffffff',
  '--toolbar-border': '#e3e0f2',
  '--toolbar-mark': '#de1b5a',
  '--toolbar-credit': '#55517d',
  '--toolbar-credit-hover': '#4336c9',
  '--toolbar-button-border': '#1a1650',
  '--toolbar-button-label': '#1a1650',
  '--toolbar-button-hover-fill': '#f1effa',
  '--toolbar-focus-ring': '#1a1650',
};

const DARK: Palette = {
  '--toolbar-ground': '#0e0a0d',
  '--toolbar-border': '#2e242c',
  '--toolbar-mark': '#e2788f',
  '--toolbar-credit': '#a3959f',
  '--toolbar-credit-hover': '#b59bdb',
  '--toolbar-button-border': '#d3cad0',
  '--toolbar-button-label': '#d3cad0',
  '--toolbar-button-hover-fill': '#251b23',
  '--toolbar-focus-ring': '#cdbcd9',
};

const VISUALLY_HIDDEN = {
  'position': 'absolute',
  'width': '1px',
  'height': '1px',
  'margin': '-1px',
  'padding': '0',
  'overflow': 'hidden',
  'clip-path': 'inset(50%)',
  'white-space': 'nowrap',
  'border': '0',
};

const resumeDir = resolve(import.meta.dirname, '../../app/components/resume');
const vueStyle = /<style>([\s\S]*)<\/style>/u.exec(
  readFileSync(resolve(resumeDir, 'ResumeDocument.vue'), 'utf8'),
)?.[1] ?? '';
const schemeStyle = readFileSync(
  resolve(resumeDir, 'resumeColorScheme.css'), 'utf8',
);

const resumeRules = parseCss(vueStyle);
const schemeRules = parseCss(schemeStyle);
const projectRules = parseCss(PROJECT_PAGE_STYLE);

function tokens(body: Readonly<Record<string, string>>): Palette {
  return Object.fromEntries(Object.entries(body)
    .filter(([name]) => name.startsWith('--toolbar-')));
}

const luminance = (hex: string): number => {
  const [red, green, blue] = [1, 3, 5].map((start) => {
    const value = Number.parseInt(hex.slice(start, start + 2), 16) / 255;
    return value <= 0.04045 ? value / 12.92 : ((value + 0.055) / 1.055) ** 2.4;
  });
  return 0.2126 * red! + 0.7152 * green! + 0.0722 * blue!;
};

const contrast = (one: string, two: string): number => {
  const [light, dark] = [luminance(one), luminance(two)].sort((a, b) => b - a);
  return (light! + 0.05) / (dark! + 0.05);
};

describe('public page bar tokens', () => {
  it('declares the light identity values on the bar', () => {
    expect(tokens(style(resumeRules, '.public-toolbar'))).toEqual(LIGHT);
  });

  it('takes the dark column for a dark page', () => {
    expect(tokens(style(schemeRules,
      '[data-color-scheme="dark"] .public-toolbar', '@media screen')))
      .toEqual(DARK);
  });

  it('takes the dark column for Match device on a dark device', () => {
    expect(tokens(style(schemeRules,
      '[data-color-scheme="system"] .public-toolbar',
      '@media screen and (prefers-color-scheme: dark)'))).toEqual(DARK);
  });

  it('keeps the dark column off the print path', () => {
    for (const rule of schemeRules) {
      if (Object.keys(tokens(rule.body)).length > 0) {
        expect(rule.at, rule.selector).toMatch(/^@media screen/u);
      }
    }
  });

  it('meets the contrast the design measured', () => {
    for (const [palette, expected] of [
      [LIGHT, [4.76, 7.34, 16.48, 14.49]],
      [DARK, [6.85, 6.89, 12.29, 10.42]],
    ] as const) {
      const ground = palette['--toolbar-ground']!;
      const label = palette['--toolbar-button-label']!;
      const measured = [
        contrast(palette['--toolbar-mark']!, ground),
        contrast(palette['--toolbar-credit']!, ground),
        contrast(label, ground),
        contrast(label, palette['--toolbar-button-hover-fill']!),
      ];
      for (const [index, value] of measured.entries()) {
        expect(value, String(index)).toBeCloseTo(expected[index]!, 1);
      }
      expect(contrast(palette['--toolbar-credit-hover']!, ground))
        .toBeGreaterThanOrEqual(4.5);
      expect(contrast(palette['--toolbar-focus-ring']!, ground))
        .toBeGreaterThanOrEqual(3);
      expect(contrast(palette['--toolbar-button-border']!, ground))
        .toBeGreaterThanOrEqual(3);
    }
  });

  it('focuses the skip link with the bar light tokens', () => {
    expect(style(resumeRules, 'a[href="#public-resume"]:focus'))
      .toMatchObject({
        background: '#ffffff',
        color: '#4336c9',
        outline: '2px solid #1a1650',
      });
  });

  it('paints the bar, mark and credit from its tokens', () => {
    expect(style(resumeRules, '.public-toolbar')).toMatchObject({
      'background': 'var(--toolbar-ground)',
      'border-bottom': '1px solid var(--toolbar-border)',
      'padding-block': '8px 7px',
    });
    expect(style(resumeRules, '.public-mark').color)
      .toBe('var(--toolbar-mark)');
    expect(style(resumeRules, '.public-credit')).toMatchObject({
      color: 'var(--toolbar-credit)',
      font: '500 13px/20px "Be Vietnam Pro", system-ui, sans-serif',
    });
    expect(style(resumeRules, '.public-credit:hover', '@media (hover: hover)'))
      .toMatchObject({ 'color': 'var(--toolbar-credit-hover)',
        'text-decoration': 'underline', 'text-underline-offset': '3px' });
  });
});

describe('public page bar buttons', () => {
  it.each(['.public-download', '.public-projects'])(
    'draws %s as a small outline pill', (selector) => {
      const button = style(resumeRules, selector);
      expect(button).toMatchObject({
        'height': '32px',
        'border': '1.5px solid var(--toolbar-button-border)',
        'border-radius': '999px',
        'background': 'transparent',
        'color': 'var(--toolbar-button-label)',
        'font': '500 14px/20px "Be Vietnam Pro", system-ui, sans-serif',
        'gap': '6px',
        'white-space': 'nowrap',
        'flex-shrink': '0',
      });
      expect(button).not.toHaveProperty('box-shadow');
      expect(style(resumeRules, `${selector}:hover`, '@media (hover: hover)'))
        .toEqual({ background: 'var(--toolbar-button-hover-fill)' });
      expect(style(resumeRules, `${selector}:focus-visible`)).toEqual({
        'outline': '2px solid var(--toolbar-focus-ring)',
        'outline-offset': '2px',
      });
      expect(style(resumeRules, selector, '@media (pointer: coarse)').height)
        .toBe('40px');
      expect(style(resumeRules, selector, '@media (forced-colors: active)'))
        .toEqual({ 'border-color': 'ButtonBorder' });
    });

  it('pads the download pill 10 px left and 12 px right', () => {
    expect(style(resumeRules, '.public-download').padding)
      .toBe('0 12px 0 10px');
    expect(style(resumeRules, '.public-projects')['padding-inline'])
      .toBe('12px');
  });

  it('keeps the visually hidden label inside the download pill', () => {
    expect(style(resumeRules, '.public-download').position).toBe('relative');
  });
});

describe('public page bar download labels', () => {
  const phone = '@media (width < 30rem)';

  it('shows only the full label from 30 rem', () => {
    expect(style(resumeRules, '.public-download-short'))
      .toEqual({ display: 'none' });
    expect(matching(resumeRules, '.public-download-full', '')).toEqual([]);
  });

  it('hides the full label visually and shows PDF below 30 rem', () => {
    expect(style(resumeRules, '.public-download-full', phone))
      .toEqual(VISUALLY_HIDDEN);
    expect(style(resumeRules, '.public-download-short', phone))
      .toEqual({ display: 'inline' });
    expect(lastIndex(resumeRules, '.public-download-short', phone))
      .toBeGreaterThan(lastIndex(resumeRules, '.public-download-short'));
  });
});

describe('public page bar row', () => {
  const phone = '@media (width < 30rem)';

  it('lays the brand and actions out in one wrapping row', () => {
    expect(style(resumeRules, '.public-toolbar-inner')).toMatchObject({
      'display': 'flex',
      'flex-wrap': 'wrap',
      'align-items': 'center',
      'justify-content': 'space-between',
      'column-gap': '16px',
      'row-gap': '8px',
      'min-height': '32px',
    });
    expect(style(resumeRules, '.public-toolbar-inner',
      '@media (width < 40rem)')).toEqual({ 'column-gap': '8px' });
    expect(style(resumeRules, '.public-toolbar-inner',
      '@media (pointer: coarse)')).toEqual({ 'min-height': '40px' });
    expect(style(resumeRules, '.public-brand').gap).toBe('8px');
    expect(style(resumeRules, '.public-actions').gap).toBe('8px');
  });

  it('narrows the credit and the pills below 30 rem', () => {
    expect(style(resumeRules, '.public-credit', phone))
      .toEqual({ 'font-size': '12px' });
    expect(style(resumeRules, '.public-download', phone))
      .toEqual({ padding: '0 10px 0 8px' });
    expect(style(resumeRules, '.public-projects', phone))
      .toEqual({ 'padding-inline': '10px' });
    for (const selector of ['.public-credit', '.public-download',
      '.public-projects']) {
      expect(lastIndex(resumeRules, selector, phone), selector)
        .toBeGreaterThan(lastIndex(resumeRules, selector));
    }
  });

  it('hides the bar in print and marks it under forced colors', () => {
    expect(style(resumeRules, '.public-toolbar', '@media print'))
      .toEqual({ display: 'none' });
    expect(style(resumeRules, '.public-mark', '@media (forced-colors: active)'))
      .toEqual({ color: 'CanvasText' });
  });
});

describe('project page bar', () => {
  const bar = '.public-project-page .public-toolbar';

  it('declares the same light tokens as the resume page bar', () => {
    expect(tokens(style(projectRules, bar))).toEqual(LIGHT);
  });

  it('takes the dark column for a dark page and Match device', () => {
    expect(tokens(style(projectRules,
      ':root[data-scheme="dark"] .public-toolbar', '@media screen')))
      .toEqual(DARK);
    expect(tokens(style(projectRules,
      ':root[data-scheme="system"] .public-toolbar',
      '@media screen and (prefers-color-scheme:dark)'))).toEqual(DARK);
  });

  it('paints the bar, mark and credit from its tokens', () => {
    expect(style(projectRules, bar)).toMatchObject({
      'background': 'var(--toolbar-ground)',
      'border-bottom': '1px solid var(--toolbar-border)',
    });
    expect(style(projectRules, '.public-project-page .public-mark').color)
      .toBe('var(--toolbar-mark)');
    expect(style(projectRules, '.public-project-page .public-credit').color)
      .toBe('var(--toolbar-credit)');
    expect(style(projectRules, '.public-project-page .public-credit:hover',
      '@media (hover:hover)').color).toBe('var(--toolbar-credit-hover)');
  });

  it('draws the back link as the bar outline pill', () => {
    const back = style(projectRules, '.public-project-page .public-back');
    expect(back).toMatchObject({
      'min-height': '32px',
      'border': '1.5px solid var(--toolbar-button-border)',
      'border-radius': '999px',
      'background': 'transparent',
      'color': 'var(--toolbar-button-label)',
    });
    expect(back).not.toHaveProperty('box-shadow');
    expect(style(projectRules, '.public-project-page .public-back:hover',
      '@media (hover:hover)')).toEqual({
      background: 'var(--toolbar-button-hover-fill)',
    });
    expect(style(projectRules,
      '.public-project-page .public-toolbar a:focus-visible'))
      .toMatchObject({ 'outline-color': 'var(--toolbar-focus-ring)' });
  });

  it('narrows the credit and the back pill below 30 rem', () => {
    const phone = '@media (width < 30rem)';
    for (const [selector, expected] of [
      ['.public-project-page .public-credit', { 'font-size': '12px' }],
      ['.public-project-page .public-back', { 'padding-inline': '8px 10px' }],
    ] as const) {
      expect(style(projectRules, selector, phone), selector).toEqual(expected);
      expect(lastIndex(projectRules, selector, phone), selector)
        .toBeGreaterThan(lastIndex(projectRules, selector));
    }
  });

  it('shares one ground with the page in both schemes', () => {
    const page = (selector: string, at = '') =>
      style(projectRules, selector, at)['--project-ground'];
    const bar = (selector: string, at = '') =>
      style(projectRules, selector, at)['--toolbar-ground'];
    expect(page(':root')).toBe(bar('.public-project-page .public-toolbar'));
    expect(page(':root[data-scheme="dark"]', '@media screen')).toBe(
      bar(':root[data-scheme="dark"] .public-toolbar', '@media screen'),
    );
    const system = '@media screen and (prefers-color-scheme:dark)';
    expect(page(':root[data-scheme="system"]', system)).toBe(
      bar(':root[data-scheme="system"] .public-toolbar', system),
    );
  });

  it('keeps the phone gap of the resume page bar', () => {
    const inner = '.public-project-page .public-toolbar-inner';
    expect(style(projectRules, inner)['column-gap']).toBe('8px');
    expect(style(projectRules, inner, '@media (min-width:40rem)'))
      .toEqual({ 'column-gap': '16px' });
  });
});
