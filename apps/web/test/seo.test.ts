import { beforeEach, describe, expect, it, vi } from 'vitest';
import {
  mockNuxtImport,
  mountSuspended,
  registerEndpoint,
} from '@nuxt/test-utils/runtime';
import { flushPromises } from '@vue/test-utils';
import { setResponseStatus } from 'h3';
import AppRoot from '../app/app.vue';
import { registerCapabilities } from './support/capabilities';
import { setSiteLocale } from './support/locale';
import { holdSignedOutRedirects } from './support/signedOutRedirects';

mockNuxtImport('navigateTo', () => vi.fn());
registerCapabilities({ providerLogin: false, agentAccess: false });
registerEndpoint('/api/v1/me', (event) => {
  setResponseStatus(event, 401);
  return { error: { code: 'session_required', message: 'Sign in.' } };
});

function meta(selector: string): string | null {
  return document.head.querySelector(selector)?.getAttribute('content') ?? null;
}

let current: Awaited<ReturnType<typeof mountSuspended>> | undefined;

/** Mounts the app at `route`; it stays mounted so its head tags stay set. */
async function visit(route: string): Promise<void> {
  current?.unmount();
  current = await mountSuspended(AppRoot, { route });
  await flushPromises();
}

beforeEach(() => {
  setSiteLocale(undefined);
  clearNuxtData();
  holdSignedOutRedirects();
});

const sitePages = [
  {
    route: '/',
    title: {
      vi: 'aboutme — CV miễn phí, riêng tư đến khi bạn muốn',
      en: 'aboutme — Free resumes, private until you choose',
    },
    description: {
      vi: 'Công cụ tạo CV mã nguồn mở.',
      en: 'Open-source resume builder.',
    },
    canonical: 'https://aboutme.vn/',
  },
  {
    route: '/privacy',
    title: {
      vi: 'Chính sách quyền riêng tư · aboutme',
      en: 'Privacy Policy · aboutme',
    },
    description: {
      vi: 'Chính sách quyền riêng tư của aboutme',
      en: 'The aboutme privacy policy',
    },
    canonical: 'https://aboutme.vn/privacy',
  },
  {
    route: '/terms',
    title: {
      vi: 'Điều khoản sử dụng · aboutme',
      en: 'Terms of Service · aboutme',
    },
    description: {
      vi: 'Điều khoản sử dụng aboutme',
      en: 'The terms for using aboutme',
    },
    canonical: 'https://aboutme.vn/terms',
  },
] as const;

describe('site page search metadata', () => {
  for (const page of sitePages) {
    for (const locale of ['vi', 'en'] as const) {
      it(`describes ${page.route} in ${locale}`, async () => {
        setSiteLocale(locale);
        await visit(page.route);

        await waitForTitle(page.title[locale]);
        expect(meta('meta[name="description"]')).toContain(
          page.description[locale],
        );
        expect(
          document.head.querySelector('link[rel="canonical"]')
            ?.getAttribute('href'),
        ).toBe(page.canonical);
        expect(meta('meta[property="og:title"]')).toBe(page.title[locale]);
        expect(meta('meta[property="og:url"]')).toBe(page.canonical);
        expect(meta('meta[property="og:type"]')).toBe('website');
        expect(meta('meta[property="og:site_name"]')).toBe('aboutme');
        expect(meta('meta[property="og:locale"]')).toBe(
          locale === 'vi' ? 'vi_VN' : 'en_US',
        );
        expect(meta('meta[property="og:locale:alternate"]')).toBe(
          locale === 'vi' ? 'en_US' : 'vi_VN',
        );
        expect(meta('meta[property="og:image"]')).toBe(
          'https://aboutme.vn/og-image.jpg',
        );
        expect(meta('meta[name="twitter:card"]')).toBe('summary_large_image');
        expect(document.head.querySelector('meta[name="robots"]')).toBeNull();
        // Stops Safari and other browsers from auto-linking digit runs such
        // as sample resume date ranges into tel: links, on every page.
        expect(meta('meta[name="format-detection"]')).toBe(
          'telephone=no, date=no, address=no, email=no',
        );
      });
    }
  }

  it('gives the homepage truthful structured data', async () => {
    await visit('/');
    await vi.waitFor(() => expect(
      document.head.querySelector('script[type="application/ld+json"]'),
    ).not.toBeNull());
    const script = document.head.querySelector(
      'script[type="application/ld+json"]',
    );
    const graph = JSON.parse(script?.textContent ?? '{}')['@graph'] as {
      '@type': string;
      [key: string]: unknown;
    }[];

    expect(graph.map((node) => node['@type'])).toEqual([
      'WebSite',
      'Organization',
      'WebApplication',
    ]);
    expect(graph[0]).toMatchObject({
      name: 'aboutme',
      url: 'https://aboutme.vn/',
      inLanguage: ['vi', 'en'],
    });
    expect(graph[1]).toMatchObject({
      logo: 'https://aboutme.vn/apple-touch-icon-v2.png',
      sameAs: ['https://github.com/dannyota/aboutme'],
    });
    expect(graph[2]).toMatchObject({
      offers: { '@type': 'Offer', 'price': '0', 'priceCurrency': 'VND' },
    });
    expect(script?.textContent).not.toMatch(
      /aggregateRating|review|userInteractionCount/iu,
    );
  });

  it('keeps structured data off the other site pages', async () => {
    await visit('/privacy');
    expect(
      document.head.querySelector('script[type="application/ld+json"]'),
    ).toBeNull();
  });
});

describe('page titles and noindex', () => {
  it.each([
    ['/login', 'Đăng nhập · aboutme', 'Sign in · aboutme'],
    ['/register', 'Tạo tài khoản · aboutme', 'Create account · aboutme'],
    [
      '/forgot-password',
      'Quên mật khẩu · aboutme',
      'Forgot password · aboutme',
    ],
    [
      '/reset-password',
      'Đặt lại mật khẩu · aboutme',
      'Reset password · aboutme',
    ],
    ['/verify-email', 'Xác minh email · aboutme', 'Verify email · aboutme'],
  ])('titles %s in both languages and keeps it out of search', async (
    route,
    viTitle,
    enTitle,
  ) => {
    await visit(route);
    await waitForTitle(viTitle);
    expect(meta('meta[name="robots"]')).toBe('noindex');

    setSiteLocale('en');
    await visit(route);
    await waitForTitle(enTitle);
    expect(meta('meta[name="robots"]')).toBe('noindex');
  });

  it.each([
    ['/authorize', 'Authorize an agent · aboutme'],
    ['/app/settings/sessions', 'Settings · aboutme'],
  ])('titles %s in English and keeps it out of search', async (
    route,
    title,
  ) => {
    setSiteLocale('en');
    await visit(route);
    await waitForTitle(title);
    expect(meta('meta[name="robots"]')).toBe('noindex');
  });

  it.each([
    ['/authorize', 'Cấp quyền cho tác nhân · aboutme'],
    ['/app/settings/sessions', 'Cài đặt · aboutme'],
  ])('uses Vietnamese for an invalid cookie on %s', async (route, title) => {
    setSiteLocale('fr');
    await visit(route);

    await waitForTitle(title);
    expect(document.documentElement.lang).toBe('vi');
    expect(meta('meta[name="robots"]')).toBe('noindex');
  });

  it('titles resumes and settings in both locales', async () => {
    setSiteLocale('vi');
    await visit('/app/resumes');
    await waitForTitle('CV · aboutme');
    expect(meta('meta[name="robots"]')).toBe('noindex');

    setSiteLocale('en');
    await visit('/app/resumes');
    await waitForTitle('Resumes · aboutme');

    setSiteLocale('vi');
    await visit('/app/settings/sessions');
    await waitForTitle('Cài đặt · aboutme');
  });
});

async function waitForTitle(title: string): Promise<void> {
  await vi.waitFor(() => expect(document.title).toBe(title));
}
