import { describe, expect, it } from 'vitest';

import { formatRelativeTime } from '../../app/utils/relativeTime';

const now = new Date('2026-09-04T12:00:00.000Z');

describe('formatRelativeTime', () => {
  it.each([
    ['under a minute', '2026-09-04T11:59:30.000Z', 'just now'],
    ['one minute', '2026-09-04T11:59:00.000Z', '1 minute ago'],
    ['minutes', '2026-09-04T11:42:00.000Z', '18 minutes ago'],
    ['one hour', '2026-09-04T11:00:00.000Z', '1 hour ago'],
    ['hours', '2026-09-04T08:00:00.000Z', '4 hours ago'],
    ['one day', '2026-09-03T12:00:00.000Z', '1 day ago'],
    ['six days', '2026-08-29T12:00:00.000Z', '6 days ago'],
    ['older dates', '2026-08-28T12:00:00.000Z', '28 Aug 2026'],
  ])('formats %s', (_name, iso, expected) => {
    expect(formatRelativeTime(iso, now)).toBe(expected);
  });

  it('uses the injected now value instead of the wall clock', () => {
    expect(formatRelativeTime('2026-09-04T11:59:30.000Z', now)).toBe(
      'just now',
    );
    expect(
      formatRelativeTime(
        '2026-09-04T11:59:30.000Z',
        new Date('2026-09-05T12:00:00.000Z'),
      ),
    ).toBe('1 day ago');
  });

  it('returns an unparseable ISO value unchanged', () => {
    expect(formatRelativeTime('not-a-date', now)).toBe('not-a-date');
  });
});
