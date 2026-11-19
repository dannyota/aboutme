// @vitest-environment node

import { describe, expect, it } from 'vitest';

import { PROJECT_PAGE_COPY } from '../../app/i18n/project-pages';
import {
  PROJECT_PAGE_STYLE,
  PROJECT_PAGE_TOKENS,
} from '../../server/workers/public-render/project-style';
import { lastIndex, parseCss, style } from './css-rules';

const luminance = (hex: string): number => {
  const channels = [1, 3, 5].map((start) =>
    Number.parseInt(hex.slice(start, start + 2), 16) / 255);
  const [red, green, blue] = channels.map((value) => (value <= 0.04045
    ? value / 12.92
    : ((value + 0.055) / 1.055) ** 2.4));
  return 0.2126 * red! + 0.7152 * green! + 0.0722 * blue!;
};

const contrast = (foreground: string, background: string): number => {
  const [light, dark] = [luminance(foreground), luminance(background)]
    .sort((a, b) => b - a);
  return (light! + 0.05) / (dark! + 0.05);
};

const EXPECTED = {
  light: {
    ground: '#ffffff', card: '#ffffff', ink: '#1a1650', muted: '#55517d',
    link: '#4336c9', primary: '#594bad', primaryHover: '#4c3c9c',
    onPrimary: '#ffffff', chip: '#f1effa', hover: '#f1effa',
    focus: '#1a1650', border: '#e3e0f2',
    hairline: 'rgba(26, 22, 80, 0.14)',
  },
  dark: {
    ground: '#0e0a0d', card: '#1b1419', ink: '#d3cad0', muted: '#a3959f',
    link: '#b59bdb', primary: '#7267c0', primaryHover: '#675bb3',
    onPrimary: '#ffffff', chip: '#151015', hover: '#251b23',
    focus: '#cdbcd9', border: '#2e242c',
    hairline: 'rgba(211, 202, 208, 0.14)',
  },
} as const;

describe('project page tokens', () => {
  it.each(['light', 'dark'] as const)('take the %s application tokens',
    (scheme) => {
      expect(PROJECT_PAGE_TOKENS[scheme]).toEqual(EXPECTED[scheme]);
    });

  it.each(['light', 'dark'] as const)('meet text contrast in %s', (scheme) => {
    const tokens = PROJECT_PAGE_TOKENS[scheme];
    const text: [string, string, string][] = [
      ['ink', 'ground', tokens.ink], ['ink', 'card', tokens.ink],
      ['ink', 'chip', tokens.ink], ['muted', 'ground', tokens.muted],
      ['muted', 'card', tokens.muted], ['muted', 'chip', tokens.muted],
      ['link', 'ground', tokens.link], ['link', 'card', tokens.link],
      ['link', 'hover', tokens.link], ['ink', 'hover', tokens.ink],
      ['onPrimary', 'primary', tokens.onPrimary],
      ['onPrimary', 'primaryHover', tokens.onPrimary],
    ];
    for (const [foreground, background, value] of text) {
      const ground = tokens[background as keyof typeof tokens];
      expect(contrast(value, ground), `${foreground} on ${background}`)
        .toBeGreaterThanOrEqual(4.5);
    }
    for (const background of ['ground', 'card'] as const) {
      expect(contrast(tokens.focus, tokens[background]), `focus ${background}`)
        .toBeGreaterThanOrEqual(3);
      expect(contrast(tokens.primary, tokens[background]),
        `button ${background}`).toBeGreaterThanOrEqual(3);
    }
  });

  it('draws flat surfaces with hairline rows and outline links', () => {
    expect(PROJECT_PAGE_STYLE).not.toMatch(/box-shadow|--project-shadow/u);
    expect(PROJECT_PAGE_STYLE).toContain(
      '.public-project-page .project-row + .project-row'
      + '{border-top:1px solid var(--project-hairline);}',
    );
    expect(PROJECT_PAGE_STYLE).toContain(
      'padding:8px 16px;border:1.5px solid var(--project-ink);'
      + 'border-radius:999px;background:transparent;'
      + 'color:var(--project-ink);',
    );
    expect(PROJECT_PAGE_STYLE).toContain(
      '.public-project-page .project-external-primary'
      + '{border-color:var(--project-primary);'
      + 'background:var(--project-primary);'
      + 'color:var(--project-on-primary);}',
    );
  });

  it('rounds standalone cards with the card radius', () => {
    const rules = parseCss(PROJECT_PAGE_STYLE);
    const page = '.public-project-page';
    expect(style(rules, page)['--project-radius-card']).toBe('22px');
    expect(style(rules, page, '@media (min-width:48rem)'))
      .toEqual({ '--project-radius-card': '26px' });
    expect(style(rules, page, '@media (min-width:64rem)'))
      .toEqual({ '--project-radius-card': '28px' });
    expect(lastIndex(rules, page, '@media (min-width:64rem)'))
      .toBeGreaterThan(lastIndex(rules, page, '@media (min-width:48rem)'));
    for (const card of ['.project-rows', '.project-facts',
      '.project-outcome', '.project-evidence-list']) {
      expect(style(rules, `${page} ${card}`)['border-radius'], card)
        .toBe('var(--project-radius-card)');
    }
  });

  it('draws evidence as one bordered card of hairline rows', () => {
    const rules = parseCss(PROJECT_PAGE_STYLE);
    const page = '.public-project-page';
    expect(style(rules, `${page} .project-evidence-list`)).toMatchObject({
      'margin': '0', 'padding': '0', 'list-style': 'none',
      'background': 'var(--project-card)',
      'border': '1px solid var(--project-border)',
    });
    expect(style(rules,
      `${page} .project-evidence-item + .project-evidence-item`))
      .toEqual({ 'border-top': '1px solid var(--project-hairline)' });
    expect(style(rules, `${page} .project-evidence-item`))
      .toMatchObject({ padding: '16px' });
    expect(style(rules, `${page} .project-evidence-item`,
      '@media (min-width:40rem)')).toEqual({ padding: '20px 24px' });
    expect(style(rules, `${page} .project-evidence-heading`)).toMatchObject({
      'font-size': '16px', 'line-height': '24px', 'font-weight': '600',
    });
    expect(style(rules, `${page} .project-evidence-label`)).toMatchObject({
      'color': 'var(--project-ink)', 'font-size': '14px',
      'line-height': '20px', 'font-weight': '500',
    });
    expect(style(rules, `${page} .project-evidence-meta`)).toMatchObject({
      'color': 'var(--project-muted)', 'font-size': '14px',
      'line-height': '20px',
    });
    expect(style(rules, `${page} .project-evidence`))
      .toMatchObject({ color: 'var(--project-link)' });
    expect(style(rules, `${page} .project-evidence-list`,
      '@media (forced-colors:active)'))
      .toEqual({ 'border-color': 'CanvasText' });
  });

  it('colors evidence with page tokens only and adds no breakpoint', () => {
    const rules = parseCss(PROJECT_PAGE_STYLE);
    const evidence = rules.filter((rule) =>
      rule.selector.includes('project-evidence'));
    expect(evidence.length).toBeGreaterThan(0);
    for (const rule of evidence) {
      for (const [property, value] of Object.entries(rule.body)) {
        if (/color|background|border|fill|stroke/u.test(property)
          && rule.at !== '@media (forced-colors:active)') {
          expect(value, `${rule.selector} ${property}`)
            .not.toMatch(/#|rgb|hsl|\b(?:red|black|white)\b/u);
        }
      }
    }
    expect([...new Set(rules.map((rule) => rule.at))].sort()).toEqual([
      '', '@media (forced-colors:active)', '@media (hover:hover)',
      '@media (min-width:40rem)', '@media (min-width:48rem)',
      '@media (min-width:56rem)', '@media (min-width:64rem)',
      '@media (pointer:coarse)', '@media (width < 30rem)', '@media print',
      '@media screen', '@media screen and (prefers-color-scheme:dark)',
    ]);
  });

  it('writes every token into the page style', () => {
    for (const scheme of ['light', 'dark'] as const) {
      for (const value of Object.values(PROJECT_PAGE_TOKENS[scheme])) {
        expect(PROJECT_PAGE_STYLE).toContain(value);
      }
    }
  });

  it('switches to dark for a dark page and, on dark devices, a system page',
    () => {
      expect(PROJECT_PAGE_STYLE).toContain(':root[data-scheme="dark"]');
      expect(PROJECT_PAGE_STYLE).toContain(
        '@media screen and (prefers-color-scheme:dark)'
        + '{:root[data-scheme="system"]',
      );
    });
});

describe('project page copy', () => {
  it('has the same keys in both languages', () => {
    expect(Object.keys(PROJECT_PAGE_COPY.vi).sort())
      .toEqual(Object.keys(PROJECT_PAGE_COPY.en).sort());
    expect(Object.keys(PROJECT_PAGE_COPY.vi.evidenceLabels).sort())
      .toEqual(Object.keys(PROJECT_PAGE_COPY.en.evidenceLabels).sort());
  });

  it('holds the page copy', () => {
    const pick = (lng: 'vi' | 'en') => {
      const copy = PROJECT_PAGE_COPY[lng];
      return [
        copy.projects, copy.outcome, copy.about, copy.role, copy.dates,
        copy.technologies, copy.otherProjects, copy.repository, copy.demo,
        copy.link, copy.credit, copy.back('Ada'), copy.evidence,
        copy.evidenceLabels.repository, copy.evidenceLabels.pullRequest,
        copy.evidenceLabels.release, copy.checked('11/2026'),
      ];
    };
    expect(pick('vi')).toEqual([
      'Dự án', 'Kết quả', 'Mô tả', 'Vai trò', 'Thời gian', 'Công nghệ',
      'Xem các dự án khác', 'Repository', 'Demo', 'Liên kết',
      'Tạo bằng aboutme.vn', 'Về CV của Ada', 'Bằng chứng trên GitHub',
      'Chủ sở hữu kho mã này', 'Tác giả của pull request đã được merge',
      'Tác giả của bản phát hành này', 'kiểm tra 11/2026',
    ]);
    expect(pick('en')).toEqual([
      'Projects', 'Outcome', 'About the project', 'Role', 'Dates',
      'Technologies', 'See other projects', 'Repository', 'Demo', 'Link',
      'Built with aboutme.vn', 'Back to Ada\'s resume', 'GitHub evidence',
      'Owner of this repository', 'Author of a merged pull request',
      'Author of this release', 'checked 11/2026',
    ]);
  });
});
