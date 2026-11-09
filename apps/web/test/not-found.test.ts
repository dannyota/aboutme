// @vitest-environment nuxt

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import {
  mockNuxtImport,
  mountSuspended,
  registerEndpoint,
} from '@nuxt/test-utils/runtime';
import { flushPromises } from '@vue/test-utils';
import { setResponseStatus } from 'h3';
import AppRoot from '../app/app.vue';
import ErrorPage from '../app/error.vue';
import { registerCapabilities } from './support/capabilities';
import { setSiteLocale } from './support/locale';
import { holdSignedOutRedirects } from './support/signedOutRedirects';

mockNuxtImport('navigateTo', () => vi.fn());
registerCapabilities({ providerLogin: false, agentAccess: false });
registerEndpoint('/api/v1/me', (event) => {
  setResponseStatus(event, 401);
  return { error: { code: 'session_required', message: 'Sign in.' } };
});
registerEndpoint('/.well-known/deployment.json', (event) => {
  setResponseStatus(event, 404);
  return null;
});

const unknownPaths = [
  '/.env',
  '/wp-admin/',
  '/.git/config',
  '/missing/nested/path',
  '/app/not-a-page',
];
const locales = [
  {
    locale: 'vi',
    title: 'Không tìm thấy trang',
    description:
      'Trang bạn tìm không tồn tại hoặc đã được di chuyển.',
    home: 'Về trang chủ',
  },
  {
    locale: 'en',
    title: 'Page not found',
    description: 'The page you are looking for does not exist or moved.',
    home: 'Go home',
  },
] as const;

let current: Awaited<ReturnType<typeof mountSuspended>> | undefined;

beforeEach(async () => {
  await clearError();
  setSiteLocale(undefined);
  clearNuxtData();
  holdSignedOutRedirects();
});

afterEach(async () => {
  current?.unmount();
  current = undefined;
  await clearError();
  setSiteLocale(undefined);
  vi.restoreAllMocks();
});

describe('unknown routes', () => {
  for (const route of unknownPaths) {
    for (const copy of locales) {
      it(`renders a 404 in ${copy.locale} for ${route}`, async () => {
        setSiteLocale(copy.locale);
        const warn = vi.spyOn(console, 'warn');
        current = await mountSuspended(AppRoot, { route });
        await flushPromises();

        const error = useError().value;
        expect(error?.statusCode).toBe(404);
        current.unmount();

        // mountSuspended overrides Nuxt's root render, including error.vue.
        current = await mountSuspended(ErrorPage, {
          route: false,
          props: { error: error! },
        });
        await flushPromises();

        const page = current.get('[data-testid="error-page"]');
        expect(page.text()).toContain(copy.title);
        expect(page.text()).toContain(copy.description);
        expect(page.text()).toContain(copy.home);
        expect(warn.mock.calls.some((args) => args.some((message) =>
          typeof message === 'string' && message.includes('No match found'),
        ))).toBe(false);
      });
    }
  }
});
