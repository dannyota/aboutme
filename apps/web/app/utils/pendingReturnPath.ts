import { validateReturnPath } from './returnPath';

// The link opens a new tab, and sessionStorage is per tab (HTML Web Storage).

const STORAGE_KEY = 'aboutme.pendingReturnPath';
const TTL_MS = 24 * 60 * 60 * 1000;

export interface PendingReturnPathStorage {
  getItem(key: string): string | null;
  setItem(key: string, value: string): void;
  removeItem(key: string): void;
}

export interface PendingReturnPathDeps {
  readonly storage?: PendingReturnPathStorage | null;
  readonly now?: () => number;
}

interface StoredEntry {
  readonly path: string;
  readonly expiresAt: number;
}

function isStoredEntry(value: unknown): value is StoredEntry {
  return typeof value === 'object' && value !== null
    && typeof (value as { path: unknown }).path === 'string'
    && typeof (value as { expiresAt: unknown }).expiresAt === 'number';
}

function parseEntry(raw: string): StoredEntry | null {
  let parsed: unknown;
  try {
    parsed = JSON.parse(raw);
  } catch {
    return null;
  }
  return isStoredEntry(parsed) ? parsed : null;
}

function resolveStorage(
  storage: PendingReturnPathStorage | null | undefined,
): PendingReturnPathStorage | null {
  if (storage !== undefined) return storage;
  try {
    return window.localStorage;
  } catch {
    return null;
  }
}

export function remember(
  path: string | null,
  deps: PendingReturnPathDeps = {},
): void {
  const storage = resolveStorage(deps.storage);
  if (storage === null) return;
  if (path === null) {
    try {
      storage.removeItem(STORAGE_KEY);
    } catch {
      // A blocked or throwing store already has nothing to clear.
    }
    return;
  }
  const now = deps.now ?? Date.now;
  try {
    const entry: StoredEntry = { path, expiresAt: now() + TTL_MS };
    storage.setItem(STORAGE_KEY, JSON.stringify(entry));
  } catch {
    // A blocked or full store falls back to plain /login on read.
  }
}

export function take(deps: PendingReturnPathDeps = {}): string | null {
  const storage = resolveStorage(deps.storage);
  if (storage === null) return null;
  let raw: string | null;
  try {
    raw = storage.getItem(STORAGE_KEY);
  } catch {
    return null;
  }
  try {
    storage.removeItem(STORAGE_KEY);
  } catch {
    // Nothing more to do with a store that cannot be cleared.
  }
  if (raw === null) return null;
  const entry = parseEntry(raw);
  const now = deps.now ?? Date.now;
  if (entry === null || entry.expiresAt <= now()) return null;
  return validateReturnPath(entry.path);
}
