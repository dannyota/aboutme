import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import {
  mockNuxtImport,
  mountSuspended,
  registerEndpoint,
} from '@nuxt/test-utils/runtime';
import { flushPromises } from '@vue/test-utils';
import { setResponseStatus } from 'h3';
import NewResumePage from '../../app/pages/app/new.vue';
import { setSiteLocale } from '../support/locale';
import { currentPath, landedOn } from '../support/routing';

// /app/new is reached from a public gallery page, so a signed-out visitor
// must land on account creation, not sign-in, carrying the sample or
// template query back with them (register.vue then returns them here).

let meStatus = 401;
const startDocumentSpy = vi.spyOn(
  await import('../../app/templates/startDocument'),
  'startDocument',
);
mockNuxtImport('navigateTo', () => vi.fn());
registerEndpoint('/api/v1/me', (event) => {
  setResponseStatus(event, meStatus);
  return { error: { code: 'session_required', message: 'Sign in.' } };
});

describe('/app/new', () => {
  // Each case really navigates now, and a page left mounted from an earlier
  // case would answer the route change too, so every mount is torn down.
  let mounted: { unmount: () => void } | null = null;

  const mountNew = async (route: string): Promise<void> => {
    mounted = await mountSuspended(NewResumePage, { route });
    await flushPromises();
  };

  beforeEach(() => {
    meStatus = 401;
    clearNuxtData();
    setSiteLocale(undefined);
    vi.mocked(navigateTo).mockClear();
    startDocumentSpy.mockClear();
  });

  afterEach(() => {
    mounted?.unmount();
    mounted = null;
  });

  it(
    'sends a signed-out sample visitor to register, next the full path',
    async () => {
      const route = '/app/new?sample=ats-plain&lng=en';
      await mountNew(route);
      const want = `/register?next=${encodeURIComponent(route)}`;
      expect(await landedOn(want)).toBe(want);
    },
  );

  it(
    'sends a signed-out template visitor to register the same way',
    async () => {
      const route = '/app/new?template=classic-serif';
      await mountNew(route);
      const want = `/register?next=${encodeURIComponent(route)}`;
      expect(await landedOn(want)).toBe(want);
      expect(currentPath()).not.toContain('/login');
    },
  );

  it('does not restart a blank document when the interface locale changes',
    async () => {
      setSiteLocale('vi');
      await mountNew('/app/new?template=classic-serif');
      const started = startDocumentSpy.mock.calls;
      expect(started).toHaveLength(1);
      const request = started[0]?.[0];

      useState('aboutme-locale').value = 'en';
      await flushPromises();

      expect(startDocumentSpy.mock.calls).toEqual([[request]]);
      expect(currentPath()).not.toContain('/app/resumes/');
    },
  );
});
