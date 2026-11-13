export const locales = ['vi', 'en'] as const;

export type Locale = (typeof locales)[number];

export const defaultLocale: Locale = 'vi';

export const localeCookie = 'aboutme-locale';

export const localeCookieMaxAgeSeconds = 60 * 60 * 24 * 365;

export const localeNames: Record<Locale, string> = {
  vi: 'Tiếng Việt',
  en: 'English',
};

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
  '/verify',
  '/showcase',
  '/guide/mcp',
  '/app/settings/sessions',
  '/authorize',
  '/app/import/linkedin',
]);

const GALLERY_PATH = /^\/templates(?:\/[a-z0-9]+(?:-[a-z0-9]+)*)?$/u;
const WORKSPACE_PATH
  = /^\/app\/(?:new|(?:resumes|views|projects)(?:\/[^/.\\?#%][^/\\?#%]*|))$/u;

export function isLocalizedPath(path: string): boolean {
  const trimmed = path.length > 1 ? path.replace(/\/+$/, '') : path;
  return localizedPaths.has(trimmed)
    || GALLERY_PATH.test(trimmed)
    || WORKSPACE_PATH.test(trimmed);
}
