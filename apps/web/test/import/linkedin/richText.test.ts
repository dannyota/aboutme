// @vitest-environment jsdom
//
// This file needs the client sanitizer (DOMPurify over jsdom) to verify the
// generated HTML round-trips unchanged, so it runs under jsdom rather than
// node, unlike dates.test.ts in this directory.

import { HOSTILE_CORPUS } from '@aboutme/schema/sanitizer';
import { describe, expect, it } from 'vitest';

import {
  clipText,
  linesToRichText,
  RICH_TEXT_MAX_BYTES,
  type TextLine,
} from '../../../app/import/linkedin/richText';
import { sanitizeRichText } from '../../../app/utils/sanitizeRichText';

const byteLength = (text: string): number =>
  new TextEncoder().encode(text).length;

const escapeForTest = (text: string): string =>
  text
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
    .replace(/'/g, '&#39;');

describe('linesToRichText paragraphs', () => {
  it('returns an empty result for no lines', () => {
    expect(linesToRichText([])).toEqual({ html: '', cut: false });
  });

  it('returns an empty result when every line is blank', () => {
    const lines: TextLine[] = [
      { text: '', gap: null },
      { text: '   ', gap: 12 },
    ];
    expect(linesToRichText(lines)).toEqual({ html: '', cut: false });
  });

  it('skips empty lines between real content', () => {
    const lines: TextLine[] = [
      { text: 'One', gap: null },
      { text: '', gap: 12 },
      { text: 'Two', gap: 12 },
    ];
    expect(linesToRichText(lines).html).toBe('<p>One Two</p>');
  });

  it('joins lines into one paragraph with a single space', () => {
    const lines: TextLine[] = [
      { text: 'Hello', gap: null },
      { text: 'world', gap: 12 },
      { text: 'foo', gap: 12 },
    ];
    expect(linesToRichText(lines)).toEqual({
      html: '<p>Hello world foo</p>',
      cut: false,
    });
  });

  it('continues the paragraph across a null-gap page break', () => {
    const lines: TextLine[] = [
      { text: 'Alpha', gap: null },
      { text: 'Beta', gap: 12 },
      { text: 'Gamma', gap: null },
    ];
    expect(linesToRichText(lines).html).toBe('<p>Alpha Beta Gamma</p>');
  });

  it('starts a new paragraph when a gap exceeds 1.3x the common gap', () => {
    const lines: TextLine[] = [
      { text: 'A', gap: null },
      { text: 'B', gap: 12 },
      { text: 'C', gap: 12 },
      { text: 'D', gap: 30 },
    ];
    expect(linesToRichText(lines).html).toBe('<p>A B C</p><p>D</p>');
  });

  it('does not split on gap alone with fewer than two non-null gaps', () => {
    const lines: TextLine[] = [
      { text: 'A', gap: null },
      { text: 'B', gap: 100 },
    ];
    expect(linesToRichText(lines).html).toBe('<p>A B</p>');
  });
});

describe('linesToRichText lists', () => {
  it.each([
    ['• Alpha', 'Alpha'],
    ['•Alpha', 'Alpha'],
    ['- Alpha', 'Alpha'],
    ['* Alpha', 'Alpha'],
    ['– Alpha', 'Alpha'],
  ])('recognizes the marker in %s', (text, expected) => {
    expect(linesToRichText([{ text, gap: null }]).html).toBe(
      `<ul><li>${expected}</li></ul>`,
    );
  });

  it('does not treat a hyphen without a following space as a bullet', () => {
    expect(linesToRichText([{ text: '-NoSpace', gap: null }]).html).toBe(
      '<p>-NoSpace</p>',
    );
  });

  it('groups a run of consecutive items into one ul', () => {
    const lines: TextLine[] = [
      { text: '• First', gap: null },
      { text: '• Second', gap: 12 },
      { text: '• Third', gap: 12 },
    ];
    expect(linesToRichText(lines).html).toBe(
      '<ul><li>First</li><li>Second</li><li>Third</li></ul>',
    );
  });

  it('continues an item on a line with no marker and no new gap', () => {
    const lines: TextLine[] = [
      { text: '• First point', gap: null },
      { text: 'continues here', gap: 12 },
      { text: '• Second point', gap: 12 },
    ];
    expect(linesToRichText(lines).html).toBe(
      '<ul><li>First point continues here</li><li>Second point</li></ul>',
    );
  });

  it('continues a bullet item across a page break (null gap)', () => {
    const lines: TextLine[] = [
      { text: '• First point', gap: null },
      { text: 'continues here', gap: null },
      { text: '• Second point', gap: 14 },
    ];
    expect(linesToRichText(lines).html).toBe(
      '<ul><li>First point continues here</li><li>Second point</li></ul>',
    );
  });

  it('ends the list on a new gap and starts a fresh paragraph', () => {
    const lines: TextLine[] = [
      { text: '• First', gap: null },
      { text: '• Second', gap: 12 },
      { text: 'After the list', gap: 40 },
    ];
    expect(linesToRichText(lines).html).toBe(
      '<ul><li>First</li><li>Second</li></ul><p>After the list</p>',
    );
  });

  it('starts a list after a paragraph without needing a paragraph gap', () => {
    const lines: TextLine[] = [
      { text: 'Intro', gap: null },
      { text: '• Item', gap: 12 },
    ];
    expect(linesToRichText(lines).html).toBe(
      '<p>Intro</p><ul><li>Item</li></ul>',
    );
  });
});

describe('linesToRichText escaping', () => {
  it('escapes HTML special characters in text nodes', () => {
    const lines: TextLine[] = [
      { text: `Tom & Jerry <b>"quoted"</b> it's`, gap: null },
    ];
    expect(linesToRichText(lines).html).toBe(
      '<p>Tom &amp; Jerry &lt;b&gt;&quot;quoted&quot;&lt;/b&gt; it&#39;s</p>',
    );
  });

  it.each([
    '<script>alert(1)</script>',
    '<img src=x onerror=alert(1)>',
    'javascript:alert(1)',
  ])('renders %s as escaped text only, never a live tag', (payload) => {
    const { html } = linesToRichText([{ text: payload, gap: null }]);
    expect(html).toBe(`<p>${escapeForTest(payload)}</p>`);
    expect(sanitizeRichText(html)).toBe(html);
  });

  it.each(HOSTILE_CORPUS)(
    'never lets $id introduce a live tag or attribute',
    ({ payload }) => {
      const { html } = linesToRichText([{ text: payload, gap: null }]);
      const withoutAllowedTags = html.replace(/<\/?(p|ul|li)>/g, '');
      expect(withoutAllowedTags).not.toMatch(/[<>]/);
      expect(sanitizeRichText(html)).toBe(html);
    },
  );

  it('outputs only p, ul, and li with no attributes', () => {
    const lines: TextLine[] = [
      { text: '<div class="x">One</div>', gap: null },
      { text: '• <a href="x">Two</a>', gap: 40 },
    ];
    const { html } = linesToRichText(lines);
    const tags = [...html.matchAll(/<\/?([a-zA-Z]+)>/g)].map((m) => m[1]);
    expect(tags.length).toBeGreaterThan(0);
    for (const tag of tags) {
      expect(['p', 'ul', 'li']).toContain(tag);
    }
    expect(sanitizeRichText(html)).toBe(html);
  });
});

describe('linesToRichText size limit', () => {
  it('exports the 16 KiB byte limit', () => {
    expect(RICH_TEXT_MAX_BYTES).toBe(16384);
  });

  it('does not cut when the output is within the limit', () => {
    const { cut } = linesToRichText([{ text: 'short text', gap: null }]);
    expect(cut).toBe(false);
  });

  it('drops later blocks entirely once the budget is used up', () => {
    const lines: TextLine[] = [
      { text: 'x'.repeat(20000), gap: null },
      { text: '• second item', gap: null },
    ];
    const { html, cut } = linesToRichText(lines);
    expect(cut).toBe(true);
    expect(html).not.toContain('second item');
    expect(html).not.toContain('<ul>');
    expect(byteLength(html)).toBeLessThanOrEqual(RICH_TEXT_MAX_BYTES);
    expect([...html].length).toBeLessThanOrEqual(RICH_TEXT_MAX_BYTES);
  });

  it('clips a paragraph past 16 KiB without splitting multibyte text', () => {
    const filler = 'a'.repeat(16000);
    const multibyte = 'Nguyễn Văn Mẫu \u{1F600} '.repeat(80);
    const line: TextLine = { text: filler + multibyte, gap: null };

    const { html, cut } = linesToRichText([line]);

    expect(cut).toBe(true);
    expect(byteLength(html)).toBeLessThanOrEqual(RICH_TEXT_MAX_BYTES);
    expect([...html].length).toBeLessThanOrEqual(RICH_TEXT_MAX_BYTES);
    expect(html.startsWith('<p>')).toBe(true);
    expect(html.endsWith('</p>')).toBe(true);

    // No lone surrogate: every high surrogate is followed by a low surrogate.
    for (let i = 0; i < html.length; i += 1) {
      const code = html.charCodeAt(i);
      if (code >= 0xd800 && code <= 0xdbff) {
        expect(html.charCodeAt(i + 1)).toBeGreaterThanOrEqual(0xdc00);
      }
    }

    expect(sanitizeRichText(html)).toBe(html);
  });

  it('clips within a list item and closes its tags', () => {
    const lines: TextLine[] = [
      { text: `• ${'y'.repeat(20000)}`, gap: null },
    ];
    const { html, cut } = linesToRichText(lines);
    expect(cut).toBe(true);
    expect(html.startsWith('<ul><li>')).toBe(true);
    expect(html.endsWith('</li></ul>')).toBe(true);
    expect(byteLength(html)).toBeLessThanOrEqual(RICH_TEXT_MAX_BYTES);
    expect(sanitizeRichText(html)).toBe(html);
  });
});

describe('clipText', () => {
  it('returns the text unchanged when within the limit', () => {
    expect(clipText('hello', 10)).toEqual({ text: 'hello', cut: false });
  });

  it('returns the text unchanged when exactly at the limit', () => {
    expect(clipText('hello', 5)).toEqual({ text: 'hello', cut: false });
  });

  it('cuts to at most max code points', () => {
    const result = clipText('hello world', 5);
    expect(result).toEqual({ text: 'hello', cut: true });
  });

  it('counts code points, not UTF-16 units', () => {
    const emoji = '\u{1F600}';
    const text = `ab${emoji}cd`;
    const result = clipText(text, 3);
    expect(result).toEqual({ text: `ab${emoji}`, cut: true });
  });

  it('never splits a surrogate pair', () => {
    const emoji = '\u{1F600}';
    const result = clipText(`a${emoji}b`, 2);
    expect(result.text).toBe(`a${emoji}`);
  });

  it('cuts plain text anywhere, even right after an ampersand', () => {
    expect(clipText('R&Dept', 3)).toEqual({ text: 'R&D', cut: true });
  });
});

describe('linesToRichText cut inside escapes', () => {
  it('never leaves a partial escape when the cut lands inside one', () => {
    for (const pad of [0, 1, 2, 3, 4]) {
      const text = `${'x'.repeat(pad)}${'&'.repeat(5000)}`;
      const { html, cut } = linesToRichText([{ text, gap: null }]);
      expect(cut).toBe(true);
      expect(html.endsWith('&amp;</p>')).toBe(true);
      expect(html).not.toMatch(/&[a-z#0-9]*<|&(?!amp;)/);
      expect(byteLength(html)).toBeLessThanOrEqual(RICH_TEXT_MAX_BYTES);
    }
  });
});
