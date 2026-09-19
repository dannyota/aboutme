import { describe, expect, it } from 'vitest';

import {
  type PendingReturnPathStorage,
  remember,
  take,
} from '../app/utils/pendingReturnPath';

const KEY = 'aboutme.pendingReturnPath';
const DAY_MS = 24 * 60 * 60 * 1000;

function memoryStorage(
  initial: Record<string, string> = {},
): PendingReturnPathStorage & { has(key: string): boolean } {
  const store = new Map(Object.entries(initial));
  return {
    getItem: (key) => (store.has(key) ? (store.get(key) as string) : null),
    setItem: (key, value) => {
      store.set(key, value);
    },
    removeItem: (key) => {
      store.delete(key);
    },
    has: (key) => store.has(key),
  };
}

function throwingStorage(): PendingReturnPathStorage {
  return {
    getItem: () => {
      throw new Error('blocked');
    },
    setItem: () => {
      throw new Error('blocked');
    },
    removeItem: () => {
      throw new Error('blocked');
    },
  };
}

describe('pendingReturnPath', () => {
  it('round-trips a validated path', () => {
    const storage = memoryStorage();
    remember('/app/resumes', { storage });
    expect(take({ storage })).toBe('/app/resumes');
  });

  it('clears the key after take', () => {
    const storage = memoryStorage();
    remember('/app/resumes', { storage });
    take({ storage });
    expect(storage.has(KEY)).toBe(false);
    expect(take({ storage })).toBeNull();
  });

  it('expires at 24 hours and clears the key', () => {
    const storage = memoryStorage();
    let now = 1_700_000_000_000;
    remember('/app/resumes', { storage, now: () => now });
    now += DAY_MS + 1;
    expect(take({ storage, now: () => now })).toBeNull();
    expect(storage.has(KEY)).toBe(false);
  });

  it('keeps an entry that has not yet expired', () => {
    const storage = memoryStorage();
    let now = 1_700_000_000_000;
    remember('/app/resumes', { storage, now: () => now });
    now += DAY_MS - 1;
    expect(take({ storage, now: () => now })).toBe('/app/resumes');
  });

  it('treats malformed JSON as absent and clears the key', () => {
    const storage = memoryStorage({ [KEY]: '{not json' });
    expect(take({ storage })).toBeNull();
    expect(storage.has(KEY)).toBe(false);
  });

  it('treats a wrong shape as absent and clears the key', () => {
    const storage = memoryStorage({
      [KEY]: JSON.stringify({ path: 42, expiresAt: 'soon' }),
    });
    expect(take({ storage })).toBeNull();
    expect(storage.has(KEY)).toBe(false);
  });

  it.each(['//evil.example', 'https://evil.example'])(
    'rejects a stored hostile path %s',
    (path) => {
      const storage = memoryStorage({
        [KEY]: JSON.stringify({ path, expiresAt: Date.now() + 1000 }),
      });
      expect(take({ storage })).toBeNull();
      expect(storage.has(KEY)).toBe(false);
    },
  );

  it('narrows an /app/new path the way validateReturnPath does', () => {
    const storage = memoryStorage();
    remember('/app/new?sample=ats-plain&lng=vi&x=1', { storage });
    expect(take({ storage })).toBe('/app/new?sample=ats-plain&lng=vi');
  });

  it('removes a stale key when remember is called with null', () => {
    const storage = memoryStorage({
      [KEY]: JSON.stringify({
        path: '/app/resumes',
        expiresAt: Date.now() + 1000,
      }),
    });
    remember(null, { storage });
    expect(storage.has(KEY)).toBe(false);
  });

  it('swallows a storage whose accessors throw', () => {
    const storage = throwingStorage();
    expect(() => remember('/app/resumes', { storage })).not.toThrow();
    expect(() => take({ storage })).not.toThrow();
    expect(take({ storage })).toBeNull();
  });

  it('falls back to no path when window.localStorage itself throws',
    () => {
      const original = Object.getOwnPropertyDescriptor(window, 'localStorage');
      Object.defineProperty(window, 'localStorage', {
        configurable: true,
        get() {
          throw new Error('blocked');
        },
      });
      try {
        expect(() => remember('/app/resumes')).not.toThrow();
        expect(() => take()).not.toThrow();
        expect(take()).toBeNull();
      } finally {
        if (original) Object.defineProperty(window, 'localStorage', original);
      }
    });
});
