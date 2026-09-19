import { computed, inject, reactive, type InjectionKey } from 'vue';

/**
 * `useFieldDrafts` — edits a field holds but has not handed to the store: a
 * date range that is not valid yet, or rich text waiting for its debounce.
 *
 * The editor page provides one registry. The save status reads it, so a held
 * edit shows as Unsaved rather than Saved, and the unsaved-navigation guard
 * flushes what it can and blocks on the rest. A field that remounts (after a
 * section switch or a collapsed card) restores its held value by key. Drafts
 * live in memory only: they can hold resume content.
 */
export interface FieldDraft {
  readonly value: unknown;
  /** Hands the held value to the store now, when that is possible. */
  readonly flush?: () => void;
}

export interface FieldDrafts {
  readonly count: Readonly<{ value: number }>;
  get(key: string): unknown;
  set(key: string, value: unknown, flush?: () => void): void;
  clear(key: string): void;
  /** Drops every draft whose key starts with `prefix` (a deleted entry). */
  clearPrefix(prefix: string): void;
  /** Flushes every flushable draft; returns whether any draft remains. */
  flushAll(): boolean;
}

export const FieldDraftsKey: InjectionKey<FieldDrafts>
  = Symbol('aboutme-field-drafts');

export function createFieldDrafts(): FieldDrafts {
  const drafts = reactive(new Map<string, FieldDraft>());
  const count = computed(() => drafts.size);
  return {
    count,
    get: (key) => drafts.get(key)?.value,
    set(key, value, flush) {
      drafts.set(key, flush === undefined ? { value } : { value, flush });
    },
    clear(key) {
      drafts.delete(key);
    },
    clearPrefix(prefix) {
      for (const key of [...drafts.keys()]) {
        if (key.startsWith(prefix)) drafts.delete(key);
      }
    },
    flushAll() {
      for (const draft of [...drafts.values()]) draft.flush?.();
      return drafts.size > 0;
    },
  };
}

/** The editor's registry, or undefined outside the editor page. */
export function useFieldDrafts(): FieldDrafts | undefined {
  return inject(FieldDraftsKey, undefined);
}
