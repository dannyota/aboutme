import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { startViewBeacon } from '../../app/public/viewBeacon';

// Layer 4 (visible time and interaction) and Layers 5-6 (token and proof of
// work) from docs/design/viewer-analytics/counting.md.

class FakeDoc extends EventTarget {
  visibilityState: 'visible' | 'hidden' = 'visible';
}

function trustedEvent(type: string): Event {
  const event = new Event(type);
  Object.defineProperty(event, 'isTrusted', { value: true });
  return event;
}

const CHALLENGE = {
  parameters: {
    algorithm: 'PBKDF2/SHA-256' as const,
    nonce: '0f1e2d3c4b5a69788796a5b4',
    salt: 'a1b2c3d4e5f60718293a4b5c',
    cost: 1000,
    keyLength: 32,
    keyPrefix: '3f9a0c',
    keySignature: '9d2c',
    data: { view: '3q2-7wAAAAAAAAAAAAAAAA' },
  },
  signature: '5e8f',
};

const DEFAULT_START_DATA = { owner: false, token: 'tok', challenge: CHALLENGE };

function okStartResponse(
  data: Record<string, unknown> = DEFAULT_START_DATA,
): Response {
  return { ok: true, json: async () => ({ data }) } as unknown as Response;
}

function notFoundResponse(): Response {
  return {
    ok: false, status: 404, json: async () => ({}),
  } as unknown as Response;
}

// Flushes the promise chain (fetch, json, then the listener setup) without
// touching fake timers, since Promise microtasks resolve independently of
// them.
async function flushMicrotasks(): Promise<void> {
  for (let i = 0; i < 10; i += 1) await Promise.resolve();
}

function setup(overrides: {
  fetchImpl?: ReturnType<typeof vi.fn>;
  solve?: ReturnType<typeof vi.fn>;
} = {}) {
  const doc = new FakeDoc();
  const target = new EventTarget();
  const collectResponse = {
    ok: true, json: async () => ({}),
  } as unknown as Response;
  const fetchImpl = overrides.fetchImpl
    ?? vi.fn(async (url: string) => (
      url.includes('/collect') ? collectResponse : okStartResponse()
    ));
  const solve = overrides.solve
    ?? vi.fn(async () => ({ counter: 412, derivedKey: '3f9a0c' }));
  startViewBeacon({
    slug: 'ada-lovelace',
    fetch: fetchImpl as unknown as typeof fetch,
    document: doc as unknown as Document,
    target,
    solve: solve as unknown as typeof import('altcha-lib').solveChallenge,
  });
  return { doc, target, fetchImpl, solve };
}

function collectCalls(fetchImpl: ReturnType<typeof vi.fn>): unknown[] {
  return fetchImpl.mock.calls.filter(
    ([url]) => String(url).includes('/collect'),
  );
}

beforeEach(() => {
  vi.useFakeTimers();
});

afterEach(() => {
  vi.useRealTimers();
});

describe('startViewBeacon', () => {
  it('sends nothing before 8 s of visible time', async () => {
    const { target, fetchImpl } = setup();
    await flushMicrotasks();
    target.dispatchEvent(trustedEvent('scroll'));
    await flushMicrotasks();
    await vi.advanceTimersByTimeAsync(7_999);
    expect(collectCalls(fetchImpl)).toHaveLength(0);
    await vi.advanceTimersByTimeAsync(1);
    expect(collectCalls(fetchImpl)).toHaveLength(1);
  });

  it('sends nothing without a trusted interaction', async () => {
    const { fetchImpl } = setup();
    await flushMicrotasks();
    await vi.advanceTimersByTimeAsync(60_000);
    expect(collectCalls(fetchImpl)).toHaveLength(0);
  });

  it('ignores an untrusted event', async () => {
    const { target, fetchImpl } = setup();
    await flushMicrotasks();
    // Programmatic dispatch is untrusted by the DOM spec unless overridden.
    target.dispatchEvent(new Event('scroll'));
    await flushMicrotasks();
    await vi.advanceTimersByTimeAsync(8_000);
    expect(collectCalls(fetchImpl)).toHaveLength(0);
  });

  it('does not count time while the page is hidden', async () => {
    const { doc, target, fetchImpl } = setup();
    await flushMicrotasks();
    target.dispatchEvent(trustedEvent('scroll'));
    await flushMicrotasks();
    await vi.advanceTimersByTimeAsync(4_000);
    doc.visibilityState = 'hidden';
    doc.dispatchEvent(new Event('visibilitychange'));
    await vi.advanceTimersByTimeAsync(60_000);
    expect(collectCalls(fetchImpl)).toHaveLength(0);
    doc.visibilityState = 'visible';
    doc.dispatchEvent(new Event('visibilitychange'));
    await vi.advanceTimersByTimeAsync(3_999);
    expect(collectCalls(fetchImpl)).toHaveLength(0);
    await vi.advanceTimersByTimeAsync(1);
    expect(collectCalls(fetchImpl)).toHaveLength(1);
  });

  it('stops silently when the owner views their own resume', async () => {
    const fetchImpl = vi.fn(async () => okStartResponse({ owner: true }));
    const { target } = setup({ fetchImpl });
    await flushMicrotasks();
    target.dispatchEvent(trustedEvent('scroll'));
    await vi.advanceTimersByTimeAsync(60_000);
    expect(collectCalls(fetchImpl)).toHaveLength(0);
  });

  it('stops silently on a 404 from start', async () => {
    const fetchImpl = vi.fn(async () => notFoundResponse());
    const { target } = setup({ fetchImpl });
    await flushMicrotasks();
    target.dispatchEvent(trustedEvent('scroll'));
    await vi.advanceTimersByTimeAsync(60_000);
    expect(collectCalls(fetchImpl)).toHaveLength(0);
  });

  it('sends collect once with the exact ViewCollectRequest body', async () => {
    const { target, fetchImpl } = setup();
    await flushMicrotasks();
    target.dispatchEvent(trustedEvent('scroll'));
    await flushMicrotasks();
    await vi.advanceTimersByTimeAsync(8_000);
    const calls = collectCalls(fetchImpl);
    expect(calls).toHaveLength(1);
    const [url, init] = fetchImpl.mock.calls.find(
      ([u]) => String(u).includes('/collect'),
    )!;
    expect(url).toBe('/api/v1/public/views/collect');
    expect(JSON.parse((init as RequestInit).body as string)).toEqual({
      token: 'tok',
      challenge: CHALLENGE,
      solution: { counter: 412, derivedKey: '3f9a0c' },
    });
    expect((init as RequestInit).credentials).toBe('same-origin');
    expect((init as RequestInit).keepalive).toBe(true);
  });

  it('never sends a second collect on further interaction', async () => {
    const { target, fetchImpl } = setup();
    await flushMicrotasks();
    target.dispatchEvent(trustedEvent('scroll'));
    await flushMicrotasks();
    await vi.advanceTimersByTimeAsync(8_000);
    expect(collectCalls(fetchImpl)).toHaveLength(1);
    target.dispatchEvent(trustedEvent('keydown'));
    target.dispatchEvent(trustedEvent('pointerdown'));
    await vi.advanceTimersByTimeAsync(60_000);
    expect(collectCalls(fetchImpl)).toHaveLength(1);
  });

  it('stops after 29 minutes without collecting', async () => {
    const { fetchImpl } = setup();
    await flushMicrotasks();
    // No interaction and no visible-time threshold reached: collect never
    // qualifies, and the beacon must give up rather than wait forever.
    await vi.advanceTimersByTimeAsync(29 * 60_000);
    expect(collectCalls(fetchImpl)).toHaveLength(0);
  });
});
