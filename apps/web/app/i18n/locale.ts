// Site languages. The homepage, the account pages (sign in, registration,
// password recovery, email verification), the Privacy Policy and Terms, and
// the template gallery are bilingual and default to Vietnamese for the
// initial community (docs/design/product.md). Every other route stays
// English.

export const locales = ['vi', 'en'] as const;

export type Locale = (typeof locales)[number];

export const defaultLocale: Locale = 'vi';

export const localeCookie = 'aboutme-locale';

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
  '/register',
  '/forgot-password',
  '/reset-password',
  '/verify-email',
  '/privacy',
  '/terms',
]);

/** The template gallery, /templates, and its template pages. */
const GALLERY_PATH = /^\/templates(?:\/[a-z0-9]+(?:-[a-z0-9]+)*)?$/u;

/** Whether a route path renders in the chosen language. */
export function isLocalizedPath(path: string): boolean {
  const trimmed = path.length > 1 ? path.replace(/\/+$/, '') : path;
  return localizedPaths.has(trimmed) || GALLERY_PATH.test(trimmed);
}
