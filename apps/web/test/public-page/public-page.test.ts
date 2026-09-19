// @vitest-environment node

import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { describe, expect, it } from 'vitest';

import {
  faviconHref,
  normalizeFaviconEmoji,
  normalizePublicTitle,
} from '../../app/utils/publicPage';

// The corpus is shared with the server's internal/publicpage tests, so the
// editor's pre-validation matches the authoritative server checks
// (docs/adr/0042-public-page-title-and-favicon.md).

interface CorpusCase {
  input: string;
  stored: string | null;
  codes: string[];
}

const corpus = JSON.parse(readFileSync(resolve(
  process.cwd(),
  '../server/internal/publicpage/testdata/public-page-corpus.json',
), 'utf8')) as {
  titles: CorpusCase[];
  emoji: CorpusCase[];
  faviconHrefs: { emoji: string; href: string }[];
};

const check = (
  normalize: (raw: string) => { value: string | null; codes: string[] },
  test: CorpusCase,
) => {
  const got = normalize(test.input);
  expect(got.codes, JSON.stringify(test.input)).toEqual(test.codes);
  if (test.codes.length === 0) {
    expect(got.value, JSON.stringify(test.input)).toBe(test.stored);
  }
};

describe('public page settings', () => {
  it('validates titles like the server', () => {
    for (const test of corpus.titles) check(normalizePublicTitle, test);
  });

  it('validates favicon emoji like the server', () => {
    for (const test of corpus.emoji) check(normalizeFaviconEmoji, test);
  });

  it('builds the same favicon href as the server', () => {
    for (const test of corpus.faviconHrefs) {
      expect(faviconHref(test.emoji)).toBe(test.href);
    }
  });
});
