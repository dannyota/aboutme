import { TEMPLATES } from '@aboutme/schema/templates';

export const DEFAULT_RETURN_PATH = '/app/resumes';

const MAX_RETURN_PATH_BYTES = 2048;
const APP_NEW_PATH = '/app/new';
const APP_NEW_LANGUAGES: ReadonlySet<string> = new Set(['en', 'vi']);
const TEMPLATE_IDS: ReadonlySet<string> = new Set(
  TEMPLATES.map((template) => template.id),
);
const ORIGIN = 'https://aboutme.invalid';

function hasControlCharacter(value: string): boolean {
  for (let index = 0; index < value.length; index += 1) {
    const code = value.charCodeAt(index);
    if (code < 0x20 || code === 0x7f) return true;
  }
  return false;
}

function plainPath(value: string): boolean {
  return value.startsWith('/') && !value.startsWith('//')
    && !value.includes('\\') && !hasControlCharacter(value);
}

function hasDotSegment(path: string): boolean {
  return path.split('/').some((segment) => segment === '.' || segment === '..');
}

function decodedPath(value: string): string | null {
  const path = value.split(/[?#]/u, 1)[0] ?? '';
  try {
    return decodeURIComponent(path);
  } catch {
    return null;
  }
}

function single(query: URLSearchParams, key: string): string | null {
  const values = query.getAll(key);
  return values.length === 1 ? values[0] ?? null : null;
}

function appNewReturnPath(query: URLSearchParams): string {
  const kept = new URLSearchParams();
  for (const key of ['sample', 'template'] as const) {
    const value = single(query, key);
    if (value !== null && TEMPLATE_IDS.has(value)) kept.set(key, value);
  }
  const lng = single(query, 'lng');
  if (lng !== null && APP_NEW_LANGUAGES.has(lng)) kept.set('lng', lng);
  const search = kept.toString();
  return search === '' ? APP_NEW_PATH : `${APP_NEW_PATH}?${search}`;
}

export function validateReturnPath(value: unknown): string | null {
  if (typeof value !== 'string' || value === '' || !plainPath(value)) {
    return null;
  }
  if (new TextEncoder().encode(value).byteLength > MAX_RETURN_PATH_BYTES) {
    return null;
  }
  const decoded = decodedPath(value);
  if (decoded === null || !plainPath(decoded) || hasDotSegment(decoded)) {
    return null;
  }
  let parsed: URL;
  try {
    parsed = new URL(value, ORIGIN);
  } catch {
    return null;
  }
  if (parsed.origin !== ORIGIN) return null;
  // vue-router matches paths case-insensitively, with an optional trailing /.
  if (decoded.replace(/\/$/u, '').toLowerCase() === APP_NEW_PATH) {
    return appNewReturnPath(parsed.searchParams);
  }
  return value;
}

const APP_ROUTE_PATTERNS: readonly RegExp[] = [
  /^\/$/u,
  /^\/login$/u,
  /^\/login\/second-factor$/u,
  /^\/register$/u,
  /^\/forgot-password$/u,
  /^\/reset-password$/u,
  /^\/verify-email$/u,
  /^\/authorize$/u,
  /^\/privacy$/u,
  /^\/terms$/u,
  /^\/templates(?:\/[^/]+)?$/u,
  /^\/app\/new$/u,
  /^\/app\/resumes(?:\/[^/]+)?$/u,
  /^\/app\/settings\/sessions$/u,
];

export function isAppRoute(path: string): boolean {
  const pathname = path.split(/[?#]/u, 1)[0] ?? '';
  return APP_ROUTE_PATTERNS.some((pattern) => pattern.test(pathname));
}

function pathOnly(path: string): string {
  const pathname = path.split(/[?#]/u, 1)[0] ?? '';
  return pathname.length > 1 ? pathname.replace(/\/+$/u, '') : pathname;
}

export function requiresSession(path: string): boolean {
  const lowered = pathOnly(path).toLowerCase();
  return lowered === '/authorize' || lowered.startsWith('/app/');
}

export function signedOutRedirect(fullPath: string): string {
  const next = validateReturnPath(fullPath);
  const base = pathOnly(fullPath).toLowerCase() === APP_NEW_PATH
    ? '/register'
    : '/login';
  return next === null ? base : `${base}?next=${encodeURIComponent(next)}`;
}
