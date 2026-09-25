import { beforeEach, describe, expect, it, vi } from 'vitest';
import {
  mockNuxtImport,
  mountSuspended,
  registerEndpoint,
} from '@nuxt/test-utils/runtime';
import { flushPromises } from '@vue/test-utils';
import { setResponseStatus } from 'h3';
import NewResumePage from '../../app/pages/app/new.vue';
import { setSiteLocale } from '../support/locale';

// /app/new is reached from a public gallery page, so a signed-out visitor
// must land on account creation, not sign-in, carrying the sample or
// template query back with them (register.vue then returns them here). The
// shared route guard (middleware/signed-in.global.ts) sends them; the page
// itself does not.

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
  beforeEach(() => {
    meStatus = 401;
    clearNuxtData();
    setSiteLocale(undefined);
    vi.mocked(navigateTo).mockClear();
    startDocumentSpy.mockClear();
  });

  it.each([
    '/app/new?sample=ats-plain&lng=en',
    '/app/new?template=classic-serif',
  ])('sends a signed-out visitor at %s to register, next the full path',
    async (route) => {
      await mountSuspended(NewResumePage, { route });
      await vi.waitFor(() => {
        expect(vi.mocked(navigateTo)).toHaveBeenCalledWith(
          `/register?next=${encodeURIComponent(route)}`,
          { replace: true },
        );
      });
      expect(vi.mocked(navigateTo)).not.toHaveBeenCalledWith(
        expect.stringContaining('/login'),
        expect.anything(),
      );
    });

  it('does not restart a blank document when the interface locale changes',
    async () => {
      setSiteLocale('vi');
      await mountSuspended(NewResumePage, {
        route: '/app/new?template=classic-serif',
      });
      await flushPromises();
      const started = startDocumentSpy.mock.calls;
      expect(started).toHaveLength(1);
      const request = started[0]?.[0];

      useState('aboutme-locale').value = 'en';
      await flushPromises();

      expect(startDocumentSpy.mock.calls).toEqual([[request]]);
      expect(vi.mocked(navigateTo)).not.toHaveBeenCalledWith(
        expect.stringContaining('/app/resumes/'),
      );
    },
  );
});
