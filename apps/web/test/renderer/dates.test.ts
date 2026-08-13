import { describe, expect, it } from 'vitest';

import {
  formatDateRange,
  formatYearMonth,
} from '../../app/components/resume/formatDate';

describe('fixed date formatting', () => {
  it.each([
    ['MM/YYYY', { y: 2024, m: 3 }, '03/2024'],
    ['Mon YYYY', { y: 2024, m: 3 }, 'Mar 2024'],
    ['YYYY', { y: 2024, m: 3 }, '2024'],
    ['MM/YYYY', { y: 2024 }, '2024'],
    ['Mon YYYY', { y: 2024 }, '2024'],
    ['YYYY', { y: 2024 }, '2024'],
  ] as const)('formats %s without locale state', (format, value, expected) => {
    expect(formatYearMonth(value, format)).toBe(expected);
  });

  it('formats closed and present ranges with an en dash', () => {
    expect(
      formatDateRange(
        { start: { y: 2020 }, end: { y: 2024, m: 2 }, present: false },
        'Mon YYYY',
      ),
    ).toBe('2020 – Feb 2024');
    expect(
      formatDateRange(
        { start: { y: 2020, m: 1 }, end: null, present: true },
        'MM/YYYY',
      ),
    ).toBe('01/2020 – Present');
  });
});
