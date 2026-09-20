// Page titles and search metadata for the site pages. Only the homepage, the
// Privacy Policy, the Terms, and the template gallery are indexable; every
// other route is noindex.
import type { Locale } from './locale';
import type { WorkspaceCopy } from './workspace';

export const siteName = 'aboutme';
export const siteOrigin = 'https://aboutme.vn';
export const ogImageUrl = `${siteOrigin}/og-image.png`;

export const indexablePaths: ReadonlySet<string> = new Set([
  '/',
  '/privacy',
  '/terms',
]);

const GALLERY_PATH = /^\/templates(?:\/[a-z0-9]+(?:-[a-z0-9]+)*)?$/u;

/** Whether search engines may index a route path. */
export function isIndexablePath(path: string): boolean {
  return indexablePaths.has(path) || GALLERY_PATH.test(path);
}

/** "<page> · aboutme", the title pattern for every page but the homepage. */
export function pageTitle(name: string): string {
  return `${name} · ${siteName}`;
}

type WorkspaceTitles = {
  readonly resumes: string;
  readonly newResume: string;
  readonly editor: string;
  readonly settings: string;
  readonly authorize: string;
};

export const workspaceTitles: WorkspaceCopy<WorkspaceTitles> = {
  vi: {
    resumes: pageTitle('CV'),
    newResume: pageTitle('Tạo CV'),
    editor: pageTitle('CV'),
    settings: pageTitle('Cài đặt'),
    authorize: pageTitle('Cấp quyền cho tác nhân'),
  },
  en: {
    resumes: pageTitle('Resumes'),
    newResume: pageTitle('New resume'),
    editor: pageTitle('Resume'),
    settings: pageTitle('Settings'),
    authorize: pageTitle('Authorize an agent'),
  },
};

export const homeTitle: Record<Locale, string> = {
  vi: 'aboutme — CV miễn phí, riêng tư đến khi bạn muốn',
  en: 'aboutme — Free resumes, private until you choose',
};

export const ogLocales: Record<Locale, string> = {
  vi: 'vi_VN',
  en: 'en_US',
};
