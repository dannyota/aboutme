import { computed, inject, reactive, type InjectionKey } from 'vue';

interface FieldDraft {
  readonly value: unknown;
  readonly flush?: () => void;
}

export interface FieldDrafts {
  readonly count: Readonly<{ value: number }>;
  get(key: string): unknown;
  set(key: string, value: unknown, flush?: () => void): void;
  clear(key: string): void;
  clearPrefix(prefix: string): void;
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

export function useFieldDrafts(): FieldDrafts | undefined {
  return inject(FieldDraftsKey, undefined);
}
