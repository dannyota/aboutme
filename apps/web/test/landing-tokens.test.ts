import { readFileSync } from 'node:fs';
import { describe, expect, it } from 'vitest';

const tokensCss = readFileSync('app/landing/styles/tokens.css', 'utf8');
const homeCss = readFileSync('app/landing/styles/home.css', 'utf8');
const heroArt = readFileSync('app/landing/components/HeroArt.vue', 'utf8');
const hero = readFileSync('app/landing/components/HomeHero.vue', 'utf8');
const HOME = 'html[data-page="home"]';

describe('home styles', () => {
  it('leaves the color tokens to the application theme', () => {
    expect(tokensCss).not.toMatch(/--[\w-]+\s*:/u);
    expect(tokensCss).not.toContain('header[data-testid="app-shell"]');
    for (const selector of selectors(tokensCss)) {
      expect(selector.startsWith(HOME), selector).toBe(true);
    }
  });

  it('paints the home logo mark with the logo mark token', () => {
    expect(tokensCss.replace(/\s+/gu, ' ')).toContain(
      `@media (forced-colors: none) { ${HOME} [data-logo-part="mark"], `
      + `${HOME} [data-logo-part="dot"] { color: var(--logo-mark); } }`,
    );
  });

  it('scopes every home rule to the home page', () => {
    for (const selector of selectors(homeCss)) {
      expect(selector, selector).toMatch(/^\.home(?:[-\s.:,]|$)/u);
    }
  });

  it('keeps the white chips inside the home page', () => {
    const home = declarations(homeCss, '.home');
    expect(home['--chip']).toBe('var(--paper)');
    expect(home['--chip-foreground']).toBe('var(--coin-ink)');
  });

  it('grounds the hero on the brand panel with panel text', () => {
    expect(plain(hero, '.hero')['background']).toBe('var(--brand-panel)');
    expect(plain(hero, '.hero')['color']).toBe('var(--panel-foreground)');
    expect(plain(hero, '.hero-signin')['color'])
      .toBe('var(--panel-foreground)');
    expect(hero).toContain('stroke="rgb(0 0 0 / 14%)"');
    expect(hero).not.toMatch(/var\(--brand(?:-hover)?\)/u);
  });

  it('draws the hero pills in panel text with an ink label', () => {
    expect(plain(homeCss, '.home-pill--white')).toMatchObject({
      'border-color': 'var(--panel-foreground)',
      'background': 'var(--panel-foreground)',
      'color': 'var(--coin-ink)',
    });
    expect(plain(homeCss, '.home-pill--ghost')).toMatchObject({
      'border-color': 'var(--panel-foreground)',
      'color': 'var(--panel-foreground)',
    });
    expect(plain(homeCss, '.home-pill--ghost:hover')).toMatchObject({
      background: 'var(--panel-foreground)',
      color: 'var(--brand-panel)',
    });
    expect(plain(homeCss, '.home-pill')).toMatchObject({
      background: 'var(--brand)',
      color: 'var(--brand-foreground)',
    });
  });

  it('lifts the hero sheet with the heavier paper shadow', () => {
    expect(heroArt).toContain('box-shadow: var(--shadow-paper-hero);');
    expect(heroArt).not.toContain('var(--shadow-paper);');
  });

  it('sets every home heading at line height 1.3', () => {
    for (const selector of ['.home-h1', '.home-h2', '.home-h2-card']) {
      expect(plain(homeCss, selector)['line-height'], selector).toBe('1.3');
    }
    expect(homeCss).not.toMatch(/line-height:\s*1\.(?:1|2)\d*;/u);
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
        found.push(...splitSelectors(prelude));
      }
      boundary = index + 1;
    } else if (source[index] === '}') {
      boundary = index + 1;
    }
  }
  return found;
}

function splitSelectors(prelude: string): string[] {
  const parts: string[] = [];
  let depth = 0;
  let start = 0;
  for (let index = 0; index < prelude.length; index += 1) {
    const char = prelude[index];
    if (char === '(') depth += 1;
    else if (char === ')') depth -= 1;
    else if (char === ',' && depth === 0) {
      parts.push(prelude.slice(start, index).trim());
      start = index + 1;
    }
  }
  parts.push(prelude.slice(start).trim());
  return parts;
}

function body(css: string, selector: string): string {
  const start = css.indexOf(`${selector} {`);
  expect(start, `missing ${selector}`).toBeGreaterThanOrEqual(0);
  const bodyStart = css.indexOf('{', start) + 1;
  return css.slice(bodyStart, css.indexOf('}', bodyStart));
}

function declarations(css: string, selector: string): Record<string, string> {
  const out: Record<string, string> = {};
  for (const match of body(css, selector)
    .matchAll(/(--[\w-]+)\s*:\s*([^;]+);/gu)) {
    out[match[1]!] = match[2]!.trim();
  }
  return out;
}

function plain(css: string, selector: string): Record<string, string> {
  const out: Record<string, string> = {};
  for (const match of body(css, selector)
    .matchAll(/(?<!-)\b([a-z-]+)\s*:\s*([^;]+);/gu)) {
    out[match[1]!] = match[2]!.trim();
  }
  return out;
}
