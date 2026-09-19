import { describe, expect, it } from 'vitest';

import {
  changedPublicPageFields,
  defaultPublicTitle,
  faviconEmojiIssue,
  graphemeCount,
  isSingleEmoji,
  publicPageIssueField,
  publicTitleIssue,
} from '../../app/editor/publicPageMeta';
import { parseSummary } from '../../app/editor/resumeApiParsing';
import { CURRENT_VERSION } from '@aboutme/schema/released';

const char = (code: number): string => String.fromCodePoint(code);
const ZWJ = char(0x200d);

describe('page title', () => {
  it('accepts a title up to 70 characters counted as people see them', () => {
    expect(publicTitleIssue('Danny from aboutme.vn')).toBeNull();
    expect(publicTitleIssue('')).toBeNull();
    expect(publicTitleIssue('a'.repeat(70))).toBeNull();
    // A ZWJ emoji is one character, however many code points it has.
    const coder = `👩${ZWJ}💻`;
    expect(graphemeCount(coder.repeat(70))).toBe(70);
    expect(publicTitleIssue(coder.repeat(70))).toBeNull();
    expect(publicTitleIssue(`  ${'a'.repeat(70)}  `)).toBeNull();
  });

  it('rejects over-length and hidden characters', () => {
    expect(publicTitleIssue('a'.repeat(71))).toBe('too_long');
    for (const code of [
      0x0007, 0x00ad, 0x200b, 0x200c, 0x200e, 0x200f, 0x202e, 0x2066,
      0xfeff,
    ]) {
      expect(publicTitleIssue(`Danny${char(code)}Resume`), code.toString(16))
        .toBe('invalid_characters');
    }
    expect(publicTitleIssue(`Danny 👩${ZWJ}💻`)).toBeNull();
  });

  it('defaults to the full name', () => {
    expect(defaultPublicTitle('Ada Lovelace')).toBe('Ada Lovelace — Resume');
    expect(defaultPublicTitle('  ')).toBe('Resume');
    expect(defaultPublicTitle(undefined)).toBe('Resume');
  });
});

describe('tab icon', () => {
  it.each([
    '🚀',
    ' 🚀 ',
    '❤️',
    '👍🏽',
    `👩${ZWJ}💻`,
    '🇻🇳',
    '🏴󠁧󠁢󠁳󠁣󠁴󠁿',
  ])('accepts exactly one emoji %j', (value) => {
    expect(isSingleEmoji(value)).toBe(true);
    expect(faviconEmojiIssue(value)).toBeNull();
  });

  it.each(['a', 'ab', '🚀🚀', '🚀a', '1', '🇻'])(
    'rejects %j',
    (value) => {
      expect(faviconEmojiIssue(value)).toBe('invalid_emoji');
    },
  );

  it('treats empty as the default icon', () => {
    expect(faviconEmojiIssue('')).toBeNull();
    expect(faviconEmojiIssue('   ')).toBeNull();
  });
});

describe('publish command fields', () => {
  const stored = { publicTitle: 'Old title', faviconEmoji: '🚀' };

  it('sends only changed fields, trimmed, and "" to clear', () => {
    expect(changedPublicPageFields(stored, {
      publicTitle: ' Old title ',
      faviconEmoji: '🚀',
    })).toEqual({});
    expect(changedPublicPageFields(stored, {
      publicTitle: 'Danny from aboutme.vn ',
      faviconEmoji: '',
    })).toEqual({ publicTitle: 'Danny from aboutme.vn', faviconEmoji: '' });
    expect(changedPublicPageFields(
      { publicTitle: null, faviconEmoji: null },
      { publicTitle: '', faviconEmoji: '' },
    )).toEqual({});
  });

  it('maps server issue paths to the two fields only', () => {
    expect(publicPageIssueField('/publicTitle')).toBe('publicTitle');
    expect(publicPageIssueField('publicTitle')).toBe('publicTitle');
    expect(publicPageIssueField('/faviconEmoji')).toBe('faviconEmoji');
    expect(publicPageIssueField('/slug')).toBeNull();
    expect(publicPageIssueField('/content/work')).toBeNull();
  });
});

describe('owner resource parsing', () => {
  const summary = {
    id: 'resume-1',
    title: 'Fixture',
    lng: 'en',
    live: false,
    downloadEnabled: false,
    seoGeoEnabled: false,
    slug: null,
    schemaVersion: CURRENT_VERSION,
    createdAt: '2026-01-01T00:00:00Z',
    updatedAt: '2026-01-01T00:00:00Z',
    revision: '1',
  };

  it('reads the tab fields, with absence and null as the default', () => {
    expect(parseSummary(summary)).toMatchObject({
      publicTitle: null,
      faviconEmoji: null,
    });
    expect(parseSummary({
      ...summary,
      publicTitle: 'Danny from aboutme.vn',
      faviconEmoji: '🚀',
    })).toMatchObject({
      publicTitle: 'Danny from aboutme.vn',
      faviconEmoji: '🚀',
    });
    expect(() => parseSummary({ ...summary, publicTitle: 7 })).toThrow();
    expect(() => parseSummary({ ...summary, faviconEmoji: false })).toThrow();
  });
});
