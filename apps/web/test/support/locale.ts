import type { Locale } from '../../app/i18n/locale';

export function setSiteLocale(value: Locale | string | undefined): void {
  document.cookie = value === undefined
    ? 'aboutme-locale=; max-age=0; path=/'
    : `aboutme-locale=${value}; path=/`;
  clearNuxtState('aboutme-locale');
}
