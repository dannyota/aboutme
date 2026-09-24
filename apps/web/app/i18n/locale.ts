// Site languages. The homepage, account pages, the Privacy Policy and Terms,
// template gallery, resume workspace, settings, and authorization are
// bilingual and default to Vietnamese (docs/design/localization.md).

export const locales = ['vi', 'en'] as const;

export type Locale = (typeof locales)[number];

export const defaultLocale: Locale = 'vi';

export const localeCookie = 'aboutme-locale';

/** One year, the cookie's `max-age` (docs/design/localization.md). */
export const localeCookieMaxAgeSeconds = 60 * 60 * 24 * 365;

export const localeNames: Record<Locale, string> = {
  vi: 'Tiếng Việt',
  en: 'English',
};

// The phone-width header shows these instead of localeNames, to fit the
// header in one line; the button's aria-label keeps the full name as the
// accessible name at every width.
export const localeShortNames: Record<Locale, string> = {
  vi: 'VI',
  en: 'EN',
};

export function isLocale(value: unknown): value is Locale {
  return locales.includes(value as Locale);
}

const localizedPaths: ReadonlySet<string> = new Set([
  '/',
  '/login',
  '/login/second-factor',
  '/register',
  '/forgot-password',
  '/reset-password',
  '/verify-email',
  '/privacy',
  '/terms',
  '/app/settings/sessions',
  '/authorize',
]);

/** The template gallery, /templates, and its template pages. */
const GALLERY_PATH = /^\/templates(?:\/[a-z0-9]+(?:-[a-z0-9]+)*)?$/u;
const WORKSPACE_PATH = /^\/app\/(?:new|resumes(?:\/[^/.\\?#%][^/\\?#%]*|))$/u;

/** Whether a route path renders in the chosen language. */
export function isLocalizedPath(path: string): boolean {
  const trimmed = path.length > 1 ? path.replace(/\/+$/, '') : path;
  return localizedPaths.has(trimmed)
    || GALLERY_PATH.test(trimmed)
    || WORKSPACE_PATH.test(trimmed);
}
