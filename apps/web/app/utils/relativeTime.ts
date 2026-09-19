import type { Locale } from '../i18n/locale';
import { resumeListCopy } from '../i18n/resume-list';

const SECOND = 1000;
const MINUTE = 60 * SECOND;
const HOUR = 60 * MINUTE;
const DAY = 24 * HOUR;

export function formatRelativeTime(
  iso: string,
  now: Date,
  locale: Locale = 'en',
): string {
  const date = new Date(iso);
  const timestamp = date.getTime();
  if (Number.isNaN(timestamp)) return iso;
  const copy = resumeListCopy[locale].relativeTime;

  const elapsed = Math.max(0, now.getTime() - timestamp);
  if (elapsed < MINUTE) return copy.justNow;
  if (elapsed < HOUR) {
    const minutes = Math.floor(elapsed / MINUTE);
    return copy.minutes(minutes);
  }
  if (elapsed < DAY) {
    const hours = Math.floor(elapsed / HOUR);
    return copy.hours(hours);
  }
  if (elapsed < 7 * DAY) {
    const days = Math.floor(elapsed / DAY);
    return copy.days(days);
  }

  return copy.date(
    date.getUTCDate(),
    date.getUTCMonth() + 1,
    date.getUTCFullYear(),
  );
}
