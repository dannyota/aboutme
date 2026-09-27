// @vitest-environment node
import { describe, expect, it } from 'vitest';
import {
  buildPageLines,
  columnBoundary,
  stripFooter,
} from '../../../app/import/linkedin/lines';
import { item, footerItem, line } from './helpers/lineFixtures';

describe('buildPageLines', () => {
  it('joins items on the same baseline into one line, ordered by x', () => {
    const lines = buildPageLines(1, [
      item('World', 40, 700, 12, 10),
      item('Hello', 20, 700, 12, 10),
    ]);
    expect(lines).toHaveLength(1);
    expect(lines[0].text).toBe('Hello World');
  });

  it('skips items whose text is empty or whitespace only', () => {
    const lines = buildPageLines(1, [
      item('Hello', 20, 700, 12),
      item('   ', 100, 700, 12),
      item('', 200, 700, 12),
    ]);
    expect(lines).toHaveLength(1);
    expect(lines[0].text).toBe('Hello');
  });

  it('inserts a space when the gap exceeds a quarter of the font size', () => {
    // font size 12, quarter is 3pt; "Hello" at x=20 width 10 ends at x=30.
    const wide = buildPageLines(1, [
      item('Hello', 20, 700, 12, 10),
      item('World', 34, 700, 12, 10), // gap 4pt > 3pt threshold
    ]);
    expect(wide[0].text).toBe('Hello World');

    const narrow = buildPageLines(1, [
      item('Hello', 20, 700, 12, 10),
      item('World', 31, 700, 12, 10), // gap 1pt <= 3pt threshold
    ]);
    expect(narrow[0].text).toBe('HelloWorld');
  });

  it('does not double a space when one side already has whitespace', () => {
    // gap 4pt > threshold, but "Hello " already ends in a space
    const lines = buildPageLines(1, [
      item('Hello ', 20, 700, 12, 12),
      item('World', 36, 700, 12, 10),
    ]);
    expect(lines[0].text).toBe('Hello World');
  });

  it('collapses runs of whitespace to one space and trims', () => {
    const lines = buildPageLines(1, [item('  Hello   World  ', 20, 700, 12)]);
    expect(lines[0].text).toBe('Hello World');
  });

  it('normalizes text to NFC', () => {
    // "Nguyễn" with the "ễ" decomposed into base letter + combining marks.
    const decomposed = 'Nguyễn';
    const lines = buildPageLines(1, [item(decomposed, 20, 700, 12)]);
    expect(lines[0].text).toBe('Nguyễn');
    expect(lines[0].text.normalize('NFC')).toBe(lines[0].text);
  });

  it('joins within 40% of the larger size, separates beyond it', () => {
    // Larger size is 12, 40% tolerance is 4.8pt.
    const joined = buildPageLines(1, [
      item('A', 20, 700, 12),
      item('B', 40, 695.5, 10),
    ]);
    expect(joined).toHaveLength(1);

    const separate = buildPageLines(1, [
      item('A', 20, 700, 12),
      item('B', 40, 694.5, 10),
    ]);
    expect(separate).toHaveLength(2);
  });

  it('rounds x, y, and size to the nearest 0.5', () => {
    const lines = buildPageLines(1, [item('Hello', 20.24, 700.1, 12.05)]);
    expect(lines[0].x).toBe(20);
    expect(lines[0].y).toBe(700);
    expect(lines[0].size).toBe(12);
  });

  it('sets line x, y, size from smallest x, first item, largest size', () => {
    const lines = buildPageLines(1, [
      item('World', 40, 700, 16), // encountered first: sets y
      item('Hello', 20, 700, 12),
    ]);
    expect(lines[0].x).toBe(20); // smallest x
    expect(lines[0].y).toBe(700); // first item's baseline
    expect(lines[0].size).toBe(16); // largest size
  });

  it('orders lines by y descending', () => {
    const lines = buildPageLines(1, [
      item('Bottom', 20, 100, 12),
      item('Top', 20, 700, 12),
      item('Middle', 20, 400, 12),
    ]);
    expect(lines.map((l) => l.text)).toEqual(['Top', 'Middle', 'Bottom']);
  });

  it('keeps a sidebar and a main line on one baseline apart', () => {
    const items = [
      item('Sample Skill', 21.6, 590.8, 10.5, 60),
      item('A summary line', 223.6, 590.8, 12, 90),
    ];
    expect(buildPageLines(1, items)).toHaveLength(1);
    const lines = buildPageLines(1, items, 211.6);
    expect(lines.map((l) => [l.x, l.text])).toEqual([
      [21.5, 'Sample Skill'],
      [223.5, 'A summary line'],
    ]);
  });
});

describe('columnBoundary', () => {
  it('is the largest item\'s left edge minus 12 pt', () => {
    expect(columnBoundary([
      item('Contact', 21.6, 737.6, 13),
      item('Sample Person', 223.6, 726.5, 26),
      item('Headline', 223.6, 705.3, 12),
    ])).toBeCloseTo(211.6);
  });

  it('is undefined for a page without text', () => {
    expect(columnBoundary([item(' ', 20, 700, 12)])).toBeUndefined();
  });
});

describe('stripFooter', () => {
  it('removes a valid footer line', () => {
    const lines = [
      line(2, 21.6, 700, 13, 'Contact'),
      line(2, 264, 40, 9, 'Page 2 of 3'),
    ];
    const result = stripFooter(lines, 2, 3);
    expect(result).toEqual([line(2, 21.6, 700, 13, 'Contact')]);
  });

  it('is undefined when the footer is missing', () => {
    const lines = [line(2, 21.6, 700, 13, 'Contact')];
    expect(stripFooter(lines, 2, 3)).toBeUndefined();
  });

  it('is undefined when there are two footer-shaped lines', () => {
    const lines = [
      line(2, 21.6, 700, 13, 'Contact'),
      line(2, 100, 40, 9, 'Page 2 of 3'),
      line(2, 300, 30, 9, 'Page 2 of 3'),
    ];
    expect(stripFooter(lines, 2, 3)).toBeUndefined();
  });

  it('is undefined when the page number is wrong', () => {
    const lines = [
      line(2, 21.6, 700, 13, 'Contact'),
      line(2, 264, 40, 9, 'Page 1 of 3'),
    ];
    expect(stripFooter(lines, 2, 3)).toBeUndefined();
  });

  it('is undefined when the total page count is wrong', () => {
    const lines = [
      line(2, 21.6, 700, 13, 'Contact'),
      line(2, 264, 40, 9, 'Page 2 of 5'),
    ];
    expect(stripFooter(lines, 2, 3)).toBeUndefined();
  });

  it('requires the footer to be at or below y 72', () => {
    const lines = [line(1, 264, 100, 9, 'Page 1 of 1')];
    expect(stripFooter(lines, 1, 1)).toBeUndefined();
  });

  it('accepts a footer built with the fixture helper', () => {
    const items = [item('Contact', 21.6, 700, 13), footerItem(1, 1)];
    const lines = buildPageLines(1, items);
    const stripped = stripFooter(lines, 1, 1);
    expect(stripped).toBeDefined();
    expect(stripped?.map((l) => l.text)).toEqual(['Contact']);
  });
});
