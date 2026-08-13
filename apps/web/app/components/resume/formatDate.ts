import type { Customization, DateRange, YearMonth } from '@aboutme/schema';

type DateFormat = Customization['dateFormat'];

const MONTHS = [
  'Jan',
  'Feb',
  'Mar',
  'Apr',
  'May',
  'Jun',
  'Jul',
  'Aug',
  'Sep',
  'Oct',
  'Nov',
  'Dec',
] as const;

export function formatYearMonth(value: YearMonth, format: DateFormat): string {
  if (value.m === undefined || format === 'YYYY') return String(value.y);
  if (format === 'MM/YYYY') {
    return `${String(value.m).padStart(2, '0')}/${value.y}`;
  }
  return `${MONTHS[value.m - 1]} ${value.y}`;
}

export function formatDateRange(value: DateRange, format: DateFormat): string {
  const start = formatYearMonth(value.start, format);
  const end = value.present
    ? 'Present'
    : value.end === null
      ? ''
      : formatYearMonth(value.end, format);
  return end === '' ? start : `${start} – ${end}`;
}
