import type { Locale } from '../../app/i18n/locale';

/**
 * Sets the site-language cookie the next mount reads, and drops the shared
 * locale state so that mount starts from the cookie. `undefined` clears it.
 */
export function setSiteLocale(value: Locale | string | undefined): void {
  document.cookie = value === undefined
    ? 'aboutme-locale=; max-age=0; path=/'
    : `aboutme-locale=${value}; path=/`;
  clearNuxtState('aboutme-locale');
}
