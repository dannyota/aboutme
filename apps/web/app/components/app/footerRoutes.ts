const FOOTER_PATHS: ReadonlySet<string> = new Set([
  '/templates',
  '/showcase',
  '/guide/mcp',
  '/terms',
  '/privacy',
  '/verify',
  '/login',
  '/login/second-factor',
  '/register',
  '/forgot-password',
  '/reset-password',
  '/verify-email',
]);

const TEMPLATE_PAGE_PATH = /^\/templates\/[a-z0-9]+(?:-[a-z0-9]+)*$/u;

export function hasSiteFooter(path: string): boolean {
  const trimmed = path.length > 1 ? path.replace(/\/+$/u, '') : path;
  return FOOTER_PATHS.has(trimmed) || TEMPLATE_PAGE_PATH.test(trimmed);
}
