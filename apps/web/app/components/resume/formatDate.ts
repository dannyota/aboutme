import type { Customization, DateRange, YearMonth } from '@aboutme/schema';
import { computed, type ComputedRef, inject, type InjectionKey } from 'vue';

type DateFormat = Customization['dateFormat'];

interface DateWords {
  readonly months: readonly string[];
  readonly present: string;
}

const WORDS: Readonly<Record<'en' | 'vi', DateWords>> = {
  en: {
    months: [
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
    ],
    present: 'Present',
  },
  vi: {
    months: Array.from({ length: 12 }, (_, index) => `thg ${index + 1}`),
    present: 'Hiện tại',
  },
};

const isVietnamese = (lng: string | null | undefined): boolean =>
  lng?.toLowerCase().split('-')[0] === 'vi';

const wordsFor = (lng: string): DateWords =>
  isVietnamese(lng) ? WORDS.vi : WORDS.en;

export function dateFormatForLanguage(
  lng: string | null | undefined,
): DateFormat {
  return isVietnamese(lng) ? 'MM/YYYY' : 'Mon YYYY';
}

export function formatYearMonth(
  value: YearMonth,
  format: DateFormat,
  lng = 'en',
): string {
  if (value.m === undefined || format === 'YYYY') return String(value.y);
  if (format === 'MM/YYYY') {
    return `${String(value.m).padStart(2, '0')}/${value.y}`;
  }
  return `${wordsFor(lng).months[value.m - 1]} ${value.y}`;
}

export function formatDateRange(
  value: DateRange,
  format: DateFormat,
  lng = 'en',
): string {
  const start = formatYearMonth(value.start, format, lng);
  const end = value.present
    ? wordsFor(lng).present
    : value.end === null
      ? ''
      : formatYearMonth(value.end, format, lng);
  return end === '' || end === start ? start : `${start} – ${end}`;
}

export const ResumeLngKey: InjectionKey<ComputedRef<string>>
  = Symbol('resumeLng');

export function useResumeLng(): ComputedRef<string> {
  return inject(ResumeLngKey, computed(() => 'en'));
}
