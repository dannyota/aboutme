// @vitest-environment node

import { existsSync, readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { describe, expect, it } from 'vitest';

import { parseCss, style } from './css-rules';

const resumeDir = resolve(import.meta.dirname, '../../app/components/resume');
const read = (name: string): string =>
  readFileSync(resolve(resumeDir, name), 'utf8');
const documentStyle = /<style>([\s\S]*)<\/style>/u.exec(
  read('ResumeDocument.vue'),
)?.[1] ?? '';
const pagesPath = resolve(resumeDir, 'publicPages.css');
const pagedPath = resolve(resumeDir, 'pagedResume.css');
const optional = (path: string, name: string): string =>
  existsSync(path) ? read(name) : '';
const pagesRules = parseCss(optional(pagesPath, 'publicPages.css'));
const pagedRules = parseCss(optional(pagedPath, 'pagedResume.css'));
const schemeRules = parseCss(read('resumeColorScheme.css'));

const SCREEN = '@media screen';
const WIDE = '@media screen and (width >= 600px)';
const PHONE = '@media screen and (width < 600px)';
const shadow = (channels: string): string => [
  `0 0 0 0.5px rgb(${channels} / .14)`,
  `0 1px 2px rgb(${channels} / .08)`,
  `0 10px 26px -8px rgb(${channels} / .2)`,
  `0 32px 60px -28px rgb(${channels} / .22)`,
].join(', ');

describe('public pages styles reach print.css', () => {
  it('imports the paged and public pages rules into the document style',
    () => {
      expect(documentStyle).toContain('@import \'./pagedResume.css\';');
      expect(documentStyle).toContain('@import \'./publicPages.css\';');
      expect(read('PagedResume.vue')).not.toContain('<style');
    });

  it('keeps the paged layout and the editor-only outlines', () => {
    expect(style(pagedRules, '.paged-resume')).toMatchObject({
      'display': 'grid',
      'gap': '24px',
      'justify-content': 'center',
    });
    expect(style(pagedRules,
      '.preview-sheet .resume-page[data-page-overflow=\'true\']'))
      .toMatchObject({ outline: '2px solid #b42318' });
    expect(style(pagedRules, '.pagination-measurement')).toMatchObject({
      position: 'fixed',
      visibility: 'hidden',
    });
  });

  it('scopes every public pages rule under the public page', () => {
    expect(pagesRules.length).toBeGreaterThan(10);
    for (const rule of pagesRules) {
      expect(rule.selector).toMatch(
        /^(?::root:has\()?\.public-resume-page(?:$|[\s[.:>)])/u,
      );
    }
  });
});

describe('public pages on screen', () => {
  it('keeps the gutter stable and the text size fixed', () => {
    expect(style(pagesRules, ':root:has(.public-resume-page)', WIDE))
      .toEqual({ 'scrollbar-gutter': 'stable' });
    expect(pagesRules.filter((rule) => rule.at !== WIDE
      && 'scrollbar-gutter' in rule.body)).toEqual([]);
    expect(style(pagesRules, '.public-resume-page .resume-page'))
      .toMatchObject({
        '-webkit-text-size-adjust': '100%',
        'text-size-adjust': '100%',
      });
  });

  it('stages the pages hidden in place, never with display none', () => {
    for (const state of ['loading', 'staging']) {
      const staged = style(pagesRules,
        `.public-resume-page .public-pages[data-state="${state}"]`, SCREEN);
      expect(staged).toMatchObject({
        visibility: 'hidden',
        position: 'absolute',
      });
      expect(staged).not.toHaveProperty('display');
    }
  });

  it('hides the continuous article on screen once the pages show', () => {
    for (const state of ['shown', 'updating']) {
      expect(style(pagesRules,
        `.public-resume-page[data-pages="${state}"] > .resume-document`,
        SCREEN)).toEqual({ display: 'none' });
    }
  });

  it('draws each sheet with a 4 px radius and the renderer shadow', () => {
    expect(style(pagesRules, '.public-resume-page .public-page-sheet',
      SCREEN)).toMatchObject({
      'border-radius': '4px',
      'box-shadow': shadow('var(--public-shadow)'),
    });
    expect(style(pagesRules, '.public-resume-page', SCREEN))
      .toMatchObject({ '--public-shadow': '74 54 22' });
    expect(style(schemeRules,
      '.public-resume-page[data-color-scheme="dark"]', SCREEN))
      .toMatchObject({ '--public-shadow': '0 0 0' });
    expect(style(schemeRules,
      '.public-resume-page[data-color-scheme="system"]',
      '@media screen and (prefers-color-scheme: dark)'))
      .toMatchObject({ '--public-shadow': '0 0 0' });
    expect(pagesRules.some((rule) => Object.values(rule.body)
      .some((value) => value.includes('--shadow-paper')))).toBe(false);
  });

  it('stacks the sheets 24 px apart on a wide screen', () => {
    expect(style(pagesRules, '.public-resume-page .paged-resume', WIDE))
      .toMatchObject({ gap: '24px' });
    expect(style(pagesRules, '.public-resume-page .public-pager', WIDE))
      .toEqual({ display: 'none' });
  });

  it('turns the untransformed root into a snapping scroller on a phone',
    () => {
      expect(style(pagesRules, '.public-resume-page .paged-resume', PHONE))
        .toMatchObject({
          'display': 'flex',
          'gap': '0',
          'overflow-x': 'auto',
          'scroll-snap-type': 'x mandatory',
          'overscroll-behavior-x': 'contain',
        });
      expect(style(pagesRules,
        '.public-resume-page .paged-resume[data-zoomed]', PHONE))
        .toEqual({ 'scroll-snap-type': 'none' });
      expect(style(pagesRules, '.public-resume-page .public-page-sheet',
        PHONE)).toMatchObject({
        'scroll-snap-align': 'center',
        'scroll-snap-stop': 'always',
      });
      expect(style(pagesRules, '.public-resume-page .public-page', PHONE))
        .toMatchObject({ flex: '0 0 100%' });
    });

  it('gives the pager buttons 44 px targets', () => {
    expect(style(pagesRules, '.public-resume-page .public-pager-button'))
      .toMatchObject({ 'min-width': '44px', 'min-height': '44px' });
  });
});

describe('public pages in print', () => {
  it('keeps the measuring copy laid out so printing never fails a run', () => {
    const hidden = [...pagesRules, ...pagedRules].filter((rule) =>
      rule.at === '@media print' && rule.body.display === 'none');
    for (const rule of hidden) {
      expect(rule.selector).not.toMatch(
        /(?:\.public-pages|\.paged-resume|\.pagination-measurement)$/u,
      );
    }
  });

  it('prints the continuous article without pages, desk or pager', () => {
    for (const selector of ['.public-resume-page .public-page',
      '.public-resume-page .public-pager']) {
      expect(style(pagesRules, selector, '@media print')).toEqual({
        display: 'none',
      });
    }
    expect(style(pagesRules, '.public-resume-page > .resume-document',
      '@media print')).toMatchObject({
      'padding': '0',
      'width': 'auto',
      'margin': '0',
      'box-shadow': 'none',
      'border-radius': '0',
    });
  });
});
