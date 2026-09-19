import type { Locale } from '@/i18n/locale';
import { resumeCreateCopy } from '@/i18n/resume-create';

export const OTHER_LANGUAGE = 'other';
export const UNSET_LANGUAGE = 'unset';

export function resumeLanguageOptionsForLocale(locale: Locale) {
  return [
    { value: 'vi', label: 'Tiếng Việt' },
    { value: 'en', label: 'English' },
    { value: OTHER_LANGUAGE, label: resumeCreateCopy[locale].language.other },
  ] as const;
}

/** The option shown for a resume that has no language yet. */
export function unsetLanguageOptionForLocale(locale: Locale) {
  return {
    value: UNSET_LANGUAGE,
    label: resumeCreateCopy[locale].language.unset,
  } as const;
}

export function languageCodeHintForLocale(locale: Locale): string {
  return resumeCreateCopy[locale].language.hint;
}

export function languageCodeErrorForLocale(locale: Locale): string {
  return resumeCreateCopy[locale].language.error;
}

const BCP47 = /^[A-Za-z]{2,8}(?:-[A-Za-z0-9]{1,8})*$/;

/** A trimmed tag the create and metadata contracts accept, or null. */
export function parseLanguageTag(input: string): string | null {
  const tag = input.trim();
  return tag.length <= 35 && BCP47.test(tag) ? tag : null;
}

/** Whether a stored language means "not determined". */
export function isUnsetLanguage(lng: string | null | undefined): boolean {
  return lng === undefined || lng === null || lng === '' || lng === 'und';
}

/** The select value for a stored language. */
export function languageChoice(lng: string | null | undefined): string {
  if (isUnsetLanguage(lng)) return UNSET_LANGUAGE;
  return lng === 'vi' || lng === 'en' ? lng : OTHER_LANGUAGE;
}
