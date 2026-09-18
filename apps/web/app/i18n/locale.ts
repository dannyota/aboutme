// Site languages. The homepage and the account pages (sign in, registration,
// password recovery, email verification) are bilingual and default to
// Vietnamese for the initial community (docs/design/product.md). Every other
// route stays English.

export const locales = ['vi', 'en'] as const;

export type Locale = (typeof locales)[number];

export const defaultLocale: Locale = 'vi';

export const localeCookie = 'aboutme-locale';

export const localeNames: Record<Locale, string> = {
  vi: 'Tiếng Việt',
  en: 'English',
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
]);

/** Whether a route path renders in the chosen language. */
export function isLocalizedPath(path: string): boolean {
  const trimmed = path.length > 1 ? path.replace(/\/+$/, '') : path;
  return localizedPaths.has(trimmed);
}
