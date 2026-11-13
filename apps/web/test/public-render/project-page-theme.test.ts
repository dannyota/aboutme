// @vitest-environment node

import { describe, expect, it } from 'vitest';

import { PROJECT_PAGE_COPY } from '../../app/i18n/project-pages';
import {
  PROJECT_PAGE_STYLE,
  PROJECT_PAGE_TOKENS,
} from '../../server/workers/public-render/project-style';

// WCAG 2 relative luminance and contrast ratio, written here independently
// of the page style.
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

// The application tokens the pages take
// (docs/design/ui/typography-and-tokens.md, "Typography and tokens").
const EXPECTED = {
  light: {
    ground: '#f9f8f5', card: '#ffffff', ink: '#101b3f', muted: '#5c6178',
    link: '#23399a', primary: '#26409c', primaryHover: '#1e3483',
    onPrimary: '#ffffff', chip: '#efede6', hover: '#eceef6',
    focus: '#26409c', mark: '#cc2649',
  },
  dark: {
    ground: '#0c1020', card: '#141a2e', ink: '#f3f1ec', muted: '#a5abbf',
    link: '#a3b6f5', primary: '#8fa6f0', primaryHover: '#a3b6f5',
    onPrimary: '#0c1020', chip: '#1a2138', hover: '#1a2138',
    focus: '#8fa6f0', mark: '#ff6b8a',
  },
} as const;

describe('project page tokens', () => {
  it.each(['light', 'dark'] as const)('take the %s application tokens',
    (scheme) => {
      const tokens = PROJECT_PAGE_TOKENS[scheme];
      for (const [role, value] of Object.entries(EXPECTED[scheme])) {
        expect(tokens[role as keyof typeof tokens], role).toBe(value);
      }
    });

  it.each(['light', 'dark'] as const)('meet text contrast in %s', (scheme) => {
    const tokens = PROJECT_PAGE_TOKENS[scheme];
    const text: [string, string, string][] = [
      ['ink', 'ground', tokens.ink], ['ink', 'card', tokens.ink],
      ['ink', 'chip', tokens.ink], ['muted', 'ground', tokens.muted],
      ['muted', 'card', tokens.muted], ['muted', 'chip', tokens.muted],
      ['link', 'ground', tokens.link], ['link', 'card', tokens.link],
      ['link', 'hover', tokens.link],
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
      expect(contrast(tokens.mark, tokens[background]), `mark ${background}`)
        .toBeGreaterThanOrEqual(3);
    }
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
  });

  it('holds the page copy', () => {
    const pick = (lng: 'vi' | 'en') => {
      const copy = PROJECT_PAGE_COPY[lng];
      return [
        copy.projects, copy.outcome, copy.about, copy.role, copy.dates,
        copy.technologies, copy.otherProjects, copy.repository, copy.demo,
        copy.link, copy.credit, copy.back('Ada'),
      ];
    };
    expect(pick('vi')).toEqual([
      'Dự án', 'Kết quả', 'Mô tả', 'Vai trò', 'Thời gian', 'Công nghệ',
      'Xem các dự án khác', 'Repository', 'Demo', 'Liên kết',
      'Tạo bằng aboutme.vn', 'Về CV của Ada',
    ]);
    expect(pick('en')).toEqual([
      'Projects', 'Outcome', 'About the project', 'Role', 'Dates',
      'Technologies', 'See other projects', 'Repository', 'Demo', 'Link',
      'Built with aboutme.vn', 'Back to Ada\'s resume',
    ]);
  });
});
