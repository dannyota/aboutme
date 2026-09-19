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

  it('localizes month names and the open end by the resume language', () => {
    expect(formatYearMonth({ y: 2024, m: 3 }, 'Mon YYYY', 'vi')).toBe(
      'thg 3 2024',
    );
    expect(formatYearMonth({ y: 2024, m: 3 }, 'MM/YYYY', 'vi')).toBe('03/2024');
    expect(
      formatDateRange(
        { start: { y: 2020, m: 1 }, end: null, present: true },
        'Mon YYYY',
        'vi-VN',
      ),
    ).toBe('thg 1 2020 – Hiện tại');
    expect(
      formatDateRange(
        { start: { y: 2020, m: 1 }, end: null, present: true },
        'MM/YYYY',
        'und',
      ),
    ).toBe('01/2020 – Present');
  });

  it('prints one value when both ends of a range read the same', () => {
    expect(
      formatDateRange(
        { start: { y: 2025 }, end: { y: 2025 }, present: false },
        'YYYY',
      ),
    ).toBe('2025');
    expect(
      formatDateRange(
        { start: { y: 2025, m: 2 }, end: { y: 2025, m: 9 }, present: false },
        'YYYY',
      ),
    ).toBe('2025');
    expect(
      formatDateRange(
        { start: { y: 2025, m: 2 }, end: { y: 2025, m: 9 }, present: false },
        'MM/YYYY',
      ),
    ).toBe('02/2025 – 09/2025');
  });
});
