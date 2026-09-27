// @vitest-environment happy-dom

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

// Wires the sign-in-to-view join invite: mounted only when the resume HTML
// carries the marker, view start succeeds, and both `owner` and `signedIn`
// are false (docs/design/viewer-analytics/sign-in-to-view.md#join-invite).

async function flushMicrotasks(): Promise<void> {
  for (let i = 0; i < 10; i += 1) await Promise.resolve();
}

function setUpPage(joinInvite: string | null): void {
  document.body.innerHTML = '';
  document.documentElement.lang = 'en';
  window.history.pushState({}, '', '/ada1');
  const root = document.createElement('main');
  root.id = 'public-resume';
  root.dataset.revision = '1';
  if (joinInvite !== null) root.dataset.joinInvite = joinInvite;
  document.body.append(root);
}

function stubFetch(startData: Record<string, unknown>): void {
  vi.stubGlobal('fetch', vi.fn(async (url: string) => {
    if (String(url).includes('/views/start')) {
      return {
        ok: true,
        json: async () => ({ data: startData }),
      } as unknown as Response;
    }
    // The realtime hydration read; failing it is harmless and silent.
    return { ok: false, status: 404, json: async () => ({}) } as Response;
  }));
}

beforeEach(() => {
  vi.useFakeTimers();
  vi.resetModules();
});

afterEach(() => {
  vi.unstubAllGlobals();
  vi.useRealTimers();
  document.body.innerHTML = '';
});

describe('public-resume-client join invite wiring', () => {
  it('mounts the invite when the marker is present and both flags are false',
    async () => {
      setUpPage('/register');
      stubFetch({ owner: false, signedIn: false, token: 't', challenge: {} });
      await import('../../app/public/public-resume.client');
      await flushMicrotasks();
      await vi.advanceTimersByTimeAsync(20_000);
      await flushMicrotasks();
      expect(document.querySelector('[role="region"]')).not.toBeNull();
    });

  it('never mounts the invite without the marker', async () => {
    setUpPage(null);
    stubFetch({ owner: false, signedIn: false, token: 't', challenge: {} });
    await import('../../app/public/public-resume.client');
    await flushMicrotasks();
    await vi.advanceTimersByTimeAsync(20_000);
    await flushMicrotasks();
    expect(document.querySelector('[role="region"]')).toBeNull();
  });

  it('never mounts the invite for the owner', async () => {
    setUpPage('/register');
    stubFetch({ owner: true, signedIn: true });
    await import('../../app/public/public-resume.client');
    await flushMicrotasks();
    await vi.advanceTimersByTimeAsync(20_000);
    await flushMicrotasks();
    expect(document.querySelector('[role="region"]')).toBeNull();
  });

  it('never mounts the invite for an already signed-in viewer', async () => {
    setUpPage('/login');
    stubFetch({ owner: false, signedIn: true, token: 't', challenge: {} });
    await import('../../app/public/public-resume.client');
    await flushMicrotasks();
    await vi.advanceTimersByTimeAsync(20_000);
    await flushMicrotasks();
    expect(document.querySelector('[role="region"]')).toBeNull();
  });
});
