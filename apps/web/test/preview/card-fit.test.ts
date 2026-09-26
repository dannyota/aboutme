// @vitest-environment node

import { describe, expect, it } from 'vitest';

import {
  accentTints,
  clusterWidth,
  cutToTwoLines,
  fitHeadline,
  fitName,
  footerSize,
  lineCount,
  linkSize,
  textWidth,
} from '../../app/components/preview/cardFit';

// docs/design/link-preview-card.md, "Fitting the text".
describe('card width estimate', () => {
  it('classes each cluster by its first code point after NFD', () => {
    expect(clusterWidth(' ')).toBe(0.23);
    expect(clusterWidth('i')).toBe(0.35);
    expect(clusterWidth('ị')).toBe(0.35);
    expect(clusterWidth('t')).toBe(0.55);
    expect(clusterWidth('ệ')).toBe(0.67);
    expect(clusterWidth('đ')).toBe(0.8);
    expect(clusterWidth('Ơ')).toBe(0.9);
    expect(clusterWidth('W')).toBe(1.06);
    expect(clusterWidth('漢')).toBe(1);
    expect(clusterWidth('あ')).toBe(1);
    expect(clusterWidth('ß')).toBe(1.26);
    expect(clusterWidth('👩‍💻')).toBe(1.26);
    expect(clusterWidth('…')).toBe(0.87);
  });

  it('measures the address prefix at 7.04em', () => {
    expect(textWidth('aboutme.vn/')).toBeCloseTo(7.04, 5);
  });
});

describe('card name size', () => {
  it('keeps a two-line name at 72 px', () => {
    expect(lineCount('Nguyễn Thị Minh Khai', 72)).toBe(2);
    expect(fitName('Nguyễn Thị Minh Khai'))
      .toEqual({ size: 72, text: 'Nguyễn Thị Minh Khai' });
    expect(fitName('Ada')).toEqual({ size: 72, text: 'Ada' });
  });

  it('steps down until the name takes two lines', () => {
    const cjk = '漢'.repeat(20);
    expect(lineCount(cjk, 58)).toBe(3);
    expect(fitName(cjk)).toEqual({ size: 52, text: cjk });
  });

  it('cuts a name that does not fit at 52 px', () => {
    const name = Array.from({ length: 20 }, () => 'Nguyễn').join(' ');
    const fitted = fitName(name);
    expect(fitted.size).toBe(52);
    expect(fitted.text.endsWith('…')).toBe(true);
    expect(fitted.text.length).toBeLessThan(name.length);
    expect(lineCount(fitted.text, 52)).toBeLessThanOrEqual(2);
    expect(name.startsWith(fitted.text.slice(0, -1))).toBe(true);
  });

  it('splits a single unbreakable run', () => {
    const cut = cutToTwoLines('a'.repeat(100), 52);
    expect(cut).toBe(`${'a'.repeat(28)}…`);
    expect(lineCount(cut, 52)).toBe(2);
  });

  it('drops trailing punctuation before the ellipsis', () => {
    const words = Array.from({ length: 40 }, (_, index) =>
      index === 8 ? 'word,' : 'word').join(' ');
    const cut = fitHeadline(words);
    expect(cut).toMatch(/[^,;:–\- ]…$/u);
    expect(cut).not.toContain(',…');
    expect(cut.endsWith('word word word word…')).toBe(true);
    expect(lineCount(cut, 32)).toBeLessThanOrEqual(2);
  });
});

describe('card headline', () => {
  it('keeps a headline of two lines or less', () => {
    expect(fitHeadline('Kỹ sư phần mềm')).toBe('Kỹ sư phần mềm');
  });

  it('cuts a long headline to two lines at 32 px', () => {
    const headline = 'Senior platform engineer '.repeat(10).trim();
    const cut = fitHeadline(headline);
    expect(cut.endsWith('…')).toBe(true);
    expect(lineCount(cut, 32)).toBe(2);
  });
});

describe('card links', () => {
  it('sizes the link in place of a name from 19 to 72 px', () => {
    expect(linkSize('abcd')).toBe(72);
    expect(linkSize('m'.repeat(30))).toBe(19);
  });

  it('sizes the footer link from 14 to 24 px', () => {
    expect(footerSize('aboutme.vn/nguyen-thi-minh-khai')).toBe(24);
    expect(footerSize(`aboutme.vn/${'m'.repeat(30)}`)).toBe(14);
    expect(footerSize('aboutme.vn')).toBe(24);
  });
});

describe('card accent tints', () => {
  it('lands black on fixed gray tints', () => {
    expect(accentTints('#000000'))
      .toEqual({ bandOuter: '18%', bandInner: '1.5%', dot: '40%' });
  });

  it('gives a lighter accent a larger share for the same lightness', () => {
    const tints = accentTints('#2f855a');
    expect(Number.parseFloat(tints.bandOuter)).toBeGreaterThan(18);
    expect(Number.parseFloat(tints.dot)).toBeLessThanOrEqual(100);
  });
});
