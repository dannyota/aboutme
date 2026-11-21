import type { Locale } from '@/i18n/locale';
import type { CheckpointSummary } from '../../../editor/historyApi';
import type { EditorHistoryCopy } from '../../../i18n/editor-history';
import { formatRelativeTime } from '../../../utils/relativeTime';
import {
  isUnsetLanguage,
  resumeLanguageOptionsForLocale,
  unsetLanguageOptionForLocale,
} from '../resumeLanguage';

const WEEK_MS = 7 * 24 * 60 * 60 * 1000;
const MONTHS = [
  'Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun',
  'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec',
];

function pad(value: number): string {
  return String(value).padStart(2, '0');
}

export function formatAbsolute(
  iso: string,
  locale: Locale,
  now: Date,
): string {
  const date = new Date(iso);
  if (Number.isNaN(date.getTime())) return iso;
  const otherYear = date.getFullYear() !== now.getFullYear();
  if (locale === 'vi') {
    const base = `${pad(date.getHours())}:${pad(date.getMinutes())}, `
      + `${date.getDate()} thg ${date.getMonth() + 1}`;
    return otherYear ? `${base}, ${date.getFullYear()}` : base;
  }
  const hours = date.getHours();
  const clock = `${hours % 12 === 0 ? 12 : hours % 12}:`
    + `${pad(date.getMinutes())} ${hours < 12 ? 'AM' : 'PM'}`;
  const day = `${MONTHS[date.getMonth()]} ${date.getDate()}`;
  return otherYear
    ? `${day}, ${date.getFullYear()}, ${clock}`
    : `${day}, ${clock}`;
}

export function formatRelativeUnderWeek(
  iso: string,
  locale: Locale,
  now: Date,
): string | null {
  const elapsed = now.getTime() - new Date(iso).getTime();
  if (!Number.isFinite(elapsed) || elapsed >= WEEK_MS) return null;
  return formatRelativeTime(iso, now, locale);
}

export function causeLine(
  entry: CheckpointSummary,
  copy: EditorHistoryCopy,
): string {
  switch (entry.cause) {
    case 'assistant':
      return entry.actor.kind === 'assistant' && entry.actor.name !== ''
        ? copy.causeAssistant(entry.actor.name)
        : copy.causeAssistantUnnamed;
    case 'restore':
      return copy.causeRestore;
    case 'accept':
      return copy.causeAccept;
    default:
      return copy.causeEdit;
  }
}

export function languageName(
  lng: string | null | undefined,
  locale: Locale,
): string {
  if (isUnsetLanguage(lng)) return unsetLanguageOptionForLocale(locale).label;
  const known = resumeLanguageOptionsForLocale(locale).find(
    (option) => option.value === lng,
  );
  return known?.label ?? (lng as string);
}
