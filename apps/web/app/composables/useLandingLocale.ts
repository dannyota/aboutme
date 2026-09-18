import { computed } from 'vue';

import {
  defaultLandingLocale,
  isLandingLocale,
  type LandingLocale,
} from '@/landing/copy';

/** Reads and sets the homepage language. The cookie lets SSR render it. */
export function useLandingLocale() {
  const preference = useCookie<string | undefined>('aboutme-locale', {
    default: () => undefined,
    path: '/',
    sameSite: 'lax',
    maxAge: 60 * 60 * 24 * 365,
    watch: 'shallow',
  });
  const locale = computed<LandingLocale>(() => isLandingLocale(preference.value)
    ? preference.value
    : defaultLandingLocale);

  function setLocale(next: LandingLocale): void {
    preference.value = next;
  }

  return { locale, setLocale };
}
