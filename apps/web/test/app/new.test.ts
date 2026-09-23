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
  beforeEach(() => {
    meStatus = 401;
    clearNuxtData();
    setSiteLocale(undefined);
    vi.mocked(navigateTo).mockClear();
    startDocumentSpy.mockClear();
  });

  it(
    'sends a signed-out sample visitor to register, next the full path',
    async () => {
      const route = '/app/new?sample=ats-plain&lng=en';
      await mountSuspended(NewResumePage, { route });
      await flushPromises();
      expect(vi.mocked(navigateTo)).toHaveBeenCalledWith(
        `/register?next=${encodeURIComponent(route)}`,
      );
    },
  );

  it(
    'sends a signed-out template visitor to register the same way',
    async () => {
      const route = '/app/new?template=classic-serif';
      await mountSuspended(NewResumePage, { route });
      await flushPromises();
      expect(vi.mocked(navigateTo)).toHaveBeenCalledWith(
        `/register?next=${encodeURIComponent(route)}`,
      );
      expect(vi.mocked(navigateTo)).not.toHaveBeenCalledWith(
        expect.stringContaining('/login'),
      );
    },
  );

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
