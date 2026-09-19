// The resume's content language: a BCP 47 tag the renderer puts on the
// document root. Creating and editing a resume offer the same choices.

export const OTHER_LANGUAGE = 'other';
export const UNSET_LANGUAGE = 'unset';

export const resumeLanguageOptions = [
  { value: 'vi', label: 'Tiếng Việt' },
  { value: 'en', label: 'English' },
  { value: OTHER_LANGUAGE, label: 'Other…' },
] as const;

/** The option shown for a resume that has no language yet. */
export const unsetLanguageOption = {
  value: UNSET_LANGUAGE,
  label: 'Not set',
} as const;

export const languageCodeHint = 'A BCP 47 tag, such as fr or zh-Hant.';
export const languageCodeError
  = 'Enter a language code, such as fr or zh-Hant.';

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
