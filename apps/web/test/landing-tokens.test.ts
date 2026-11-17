import { existsSync, readFileSync } from 'node:fs';

import { mount } from '@vue/test-utils';
import { describe, expect, it } from 'vitest';

import AppLogo from '../app/components/app/AppLogo.vue';

const homePage = readFileSync('app/pages/index.vue', 'utf8');
const themeCss = readFileSync('app/assets/css/theme.css', 'utf8');
const homeCss = readFileSync('app/landing/styles/home.css', 'utf8');
const heroArt = readFileSync('app/landing/components/HeroArt.vue', 'utf8');
const hero = readFileSync('app/landing/components/HomeHero.vue', 'utf8');
const HOME = 'html[data-page="home"]';

describe('home styles', () => {
  it('loads only the scoped home stylesheet', () => {
    expect(existsSync('app/landing/styles/tokens.css')).toBe(false);
    expect(homePage.match(/<style\b[^>]*>/gu))
      .toEqual(['<style src="../landing/styles/home.css">']);
    expect(homeCss).not.toContain(HOME);
  });

  it('paints the home logo mark through AppLogo with the logo mark token',
    () => {
      const svg = mount(AppLogo).element as Element;
      const painted = Array.from(svg.querySelectorAll('.text-logo-mark'),
        (part) => part.getAttribute('data-logo-part'));

      expect(painted).toEqual(['mark', 'dot']);
      expect(themeCss).toMatch(
        /@theme inline \{[^}]*--color-logo-mark: var\(--logo-mark\);/u,
      );
      expect(homeCss).not.toContain('data-logo-part');
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
