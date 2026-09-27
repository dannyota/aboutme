// @vitest-environment node

import { describe, expect, it } from 'vitest';

import {
  educationDates,
  isDurationLine,
  matchDateLine,
  parseDateRange,
  parseLinkedInDate,
  workDates,
} from '../../../app/import/linkedin/dates';

describe('parseLinkedInDate', () => {
  it('parses a bare year', () => {
    expect(parseLinkedInDate('2020')).toEqual({ y: 2020 });
  });

  it('parses an abbreviated month and year', () => {
    expect(parseLinkedInDate('Jan 2020')).toEqual({ y: 2020, m: 1 });
  });

  it('parses a full month name and year', () => {
    expect(parseLinkedInDate('January 2020')).toEqual({ y: 2020, m: 1 });
  });

  it('accepts Sept as a month name', () => {
    expect(parseLinkedInDate('Sept 2019')).toEqual({ y: 2019, m: 9 });
  });

  it('is case insensitive on the month name', () => {
    expect(parseLinkedInDate('jANUARY 2020')).toEqual({ y: 2020, m: 1 });
    expect(parseLinkedInDate('JAN 2020')).toEqual({ y: 2020, m: 1 });
  });

  it('trims surrounding whitespace', () => {
    expect(parseLinkedInDate('  Jan 2020  ')).toEqual({ y: 2020, m: 1 });
    expect(parseLinkedInDate('  2020  ')).toEqual({ y: 2020 });
  });

  it('rejects a year below 1900 or above 2100', () => {
    expect(parseLinkedInDate('1899')).toBeUndefined();
    expect(parseLinkedInDate('2101')).toBeUndefined();
  });

  it('accepts the schema boundary years', () => {
    expect(parseLinkedInDate('1900')).toEqual({ y: 1900 });
    expect(parseLinkedInDate('2100')).toEqual({ y: 2100 });
  });

  it('rejects a malformed day/month/year shape', () => {
    expect(parseLinkedInDate('13/2020')).toBeUndefined();
  });

  it('rejects an unrecognized month word', () => {
    expect(parseLinkedInDate('Janu 2020')).toBeUndefined();
  });

  it('rejects trailing garbage after the year', () => {
    expect(parseLinkedInDate('2020a')).toBeUndefined();
  });

  it('rejects empty text', () => {
    expect(parseLinkedInDate('')).toBeUndefined();
    expect(parseLinkedInDate('   ')).toBeUndefined();
  });
});

describe('parseDateRange', () => {
  it('parses a full month range with a spaced hyphen', () => {
    expect(parseDateRange('July 2019 - August 2021')).toEqual({
      kind: 'range',
      dates: {
        start: { y: 2019, m: 7 },
        end: { y: 2021, m: 8 },
        present: false,
      },
    });
  });

  it('parses a range with an en dash', () => {
    expect(parseDateRange('Jan 2020 – Present')).toEqual({
      kind: 'range',
      dates: { start: { y: 2020, m: 1 }, end: null, present: true },
    });
  });

  it('is case insensitive on Present', () => {
    expect(parseDateRange('Jan 2020 - present')).toEqual({
      kind: 'range',
      dates: { start: { y: 2020, m: 1 }, end: null, present: true },
    });
  });

  it('parses a bare year range', () => {
    expect(parseDateRange('2018 - 2019')).toEqual({
      kind: 'range',
      dates: { start: { y: 2018 }, end: { y: 2019 }, present: false },
    });
  });

  it('allows the same month for start and end', () => {
    expect(parseDateRange('Jan 2020 - Jan 2020')).toEqual({
      kind: 'range',
      dates: {
        start: { y: 2020, m: 1 },
        end: { y: 2020, m: 1 },
        present: false,
      },
    });
  });

  it('compares a year-only start to a month end by year then month', () => {
    expect(parseDateRange('2020 - Feb 2020')).toEqual({
      kind: 'range',
      dates: { start: { y: 2020 }, end: { y: 2020, m: 2 }, present: false },
    });
  });

  it('is unreadable when the start is after the end', () => {
    expect(parseDateRange('Aug 2021 - Jan 2020')).toEqual({
      kind: 'unreadable',
    });
  });

  it('is unreadable when the start year is after the end year', () => {
    expect(parseDateRange('2021 - 2019')).toEqual({ kind: 'unreadable' });
  });

  it('is unreadable when either side fails to parse', () => {
    expect(parseDateRange('Foo 2020 - Bar 2021')).toEqual({
      kind: 'unreadable',
    });
  });

  it('is unreadable when a year is out of the schema range', () => {
    expect(parseDateRange('January 1850 - March 1851')).toEqual({
      kind: 'unreadable',
    });
  });

  it('returns startOnly for a single date', () => {
    expect(parseDateRange('March 2020')).toEqual({
      kind: 'startOnly',
      start: { y: 2020, m: 3 },
    });
  });

  it('is unreadable for text that parses as neither a range nor a date', () => {
    expect(parseDateRange('Not a date')).toEqual({ kind: 'unreadable' });
  });
});

describe('matchDateLine', () => {
  it.each([
    ['July 2019 - August 2021 (2 years 2 months)', 'July 2019 - August 2021'],
    ['Jan 2020 – Present', 'Jan 2020 – Present'],
    ['2018 - 2019', '2018 - 2019'],
    ['Foo 2020 - Bar 2021 (1 year)', 'Foo 2020 - Bar 2021'],
    [
      'January 1850 - March 1851',
      'January 1850 - March 1851',
    ],
    ['March 2020 (1 month)', 'March 2020'],
    ['May 2021 - Present (less than a year)', 'May 2021 - Present'],
  ])('matches %s', (line, datesText) => {
    expect(matchDateLine(line)).toEqual({ datesText });
  });

  it.each([
    ['2020'],
    ['Led a 2020 - 2021 migration'],
    ['Page 1 of 3'],
    ['Spring 2020'],
    ['Summer 2020 (internship)'],
    ['2018 - 2019 (Remote)'],
  ])('does not match %s', (line) => {
    expect(matchDateLine(line)).toBeUndefined();
  });
});

describe('isDurationLine', () => {
  it.each([
    '2 years 1 month',
    '9 months',
    '1 year',
    'less than a year',
    'Less Than A Year',
    '9 years 9 month',
    '1 years 2 month',
  ])('accepts %s', (text) => {
    expect(isDurationLine(text)).toBe(true);
  });

  it.each([
    '(9 months)',
    '2 years 1 month extra',
    'a year',
    '',
  ])('rejects %s', (text) => {
    expect(isDurationLine(text)).toBe(false);
  });
});

describe('workDates', () => {
  it('keeps a range as is', () => {
    const parsed = parseDateRange('2018 - 2019');
    expect(workDates(parsed)).toEqual({
      dates: { start: { y: 2018 }, end: { y: 2019 }, present: false },
    });
  });

  it('drops a startOnly date with a notice', () => {
    expect(workDates({ kind: 'startOnly', start: { y: 2020 } })).toEqual({
      notice: 'startOnly',
    });
  });

  it('drops an unreadable date with a notice', () => {
    expect(workDates({ kind: 'unreadable' })).toEqual({
      notice: 'unreadable',
    });
  });
});

describe('educationDates', () => {
  it('keeps a range as is', () => {
    const parsed = parseDateRange('2018 - 2019');
    expect(educationDates(parsed)).toEqual({
      dates: { start: { y: 2018 }, end: { y: 2019 }, present: false },
    });
  });

  it('turns a startOnly date into a graduation year range', () => {
    expect(educationDates({ kind: 'startOnly', start: { y: 2020 } })).toEqual({
      dates: { start: { y: 2020 }, end: { y: 2020 }, present: false },
    });
  });

  it('drops an unreadable date with a notice', () => {
    expect(educationDates({ kind: 'unreadable' })).toEqual({
      notice: 'unreadable',
    });
  });
});
