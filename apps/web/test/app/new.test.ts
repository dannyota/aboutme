import { beforeEach, describe, expect, it, vi } from 'vitest';
import {
  mockNuxtImport,
  mountSuspended,
  registerEndpoint,
} from '@nuxt/test-utils/runtime';
import { flushPromises } from '@vue/test-utils';
import { setResponseStatus } from 'h3';
import NewResumePage from '../../app/pages/app/new.vue';

// /app/new is reached from a public gallery page, so a signed-out visitor
// must land on account creation, not sign-in, carrying the sample or
// template query back with them (register.vue then returns them here).

let meStatus = 401;
mockNuxtImport('navigateTo', () => vi.fn());
registerEndpoint('/api/v1/me', (event) => {
  setResponseStatus(event, meStatus);
  return { error: { code: 'session_required', message: 'Sign in.' } };
});

describe('/app/new', () => {
  beforeEach(() => {
    meStatus = 401;
    clearNuxtData();
    vi.mocked(navigateTo).mockClear();
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
});
