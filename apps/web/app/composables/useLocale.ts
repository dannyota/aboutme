import { computed } from 'vue';

import {
  defaultLocale,
  isLocale,
  isLocalizedPath,
  type Locale,
  localeCookie,
  localeCookieMaxAgeSeconds,
} from '@/i18n/locale';

/**
 * Reads and sets the site language. The cookie lets SSR render the chosen
 * language; shared state keeps the header toggle and the page in step.
 */
export function useLocale() {
  const preference = useCookie<string | undefined>(localeCookie, {
    default: () => undefined,
    path: '/',
    sameSite: 'lax',
    maxAge: localeCookieMaxAgeSeconds,
  });
  const state = useState<Locale>(localeCookie, () =>
    isLocale(preference.value) ? preference.value : defaultLocale);
  // Cleared state (clearNuxtState) falls back to the default language.
  const locale = computed<Locale>(() =>
    isLocale(state.value) ? state.value : defaultLocale);

  function setLocale(next: Locale): void {
    state.value = next;
    preference.value = next;
  }

  return { locale, setLocale };
}

/** The language for the current route: the chosen one, or English. */
export function useRouteLocale() {
  const route = useRoute();
  const { locale } = useLocale();
  return computed<Locale>(() =>
    isLocalizedPath(route.path) ? locale.value : 'en');
}
