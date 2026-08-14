import { describe, expect, it } from 'vitest';

import {
  compareRevision,
  parentETag,
  parseParentETag,
  parseRevision,
} from '../../app/editor/revision';

describe('revision values', () => {
  it.each(['1', '42', '9223372036854775807'])(
    'accepts revision %s',
    (value) => {
      const revision = parseRevision(value);
      expect(revision).toBe(value);
      expect(parseParentETag(`"r${value}"`)).toBe(`"r${value}"`);
      expect(parentETag(revision)).toBe(`"r${value}"`);
    },
  );

  it.each([
    0,
    '0',
    '01',
    '+1',
    ' 1',
    '1 ',
    '1.0',
    '1e2',
    '9223372036854775808',
  ])('rejects non-canonical revision %j', (value) => {
    expect(() => parseRevision(value)).toThrow();
  });

  it.each([
    null,
    '',
    'r1',
    '"1"',
    'W/"r1"',
    '"r01"',
    '"r0"',
    '"r9223372036854775808"',
    '"r1" ',
  ])('rejects malformed parent ETag %j', (value) => {
    expect(() => parseParentETag(value)).toThrow();
  });

  it('compares canonical revisions without coercing to numbers', () => {
    expect(compareRevision(parseRevision('9'), parseRevision('10'))).toBe(-1);
    expect(compareRevision(parseRevision('42'), parseRevision('42'))).toBe(0);
    expect(
      compareRevision(
        parseRevision('9223372036854775807'),
        parseRevision('9223372036854775806'),
      ),
    ).toBe(1);
  });
});
