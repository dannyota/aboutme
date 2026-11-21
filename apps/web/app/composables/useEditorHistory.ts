import { computed, nextTick, ref, watch, type Ref } from 'vue';

import {
  createHistoryController,
  type HistoryController,
} from '../editor/historyController';
import type { SaveState } from '../editor/types';
import type { ResumeRecord } from '../stores/resumes';
import { useResumeStore } from '../stores/resumes';
import { useAuth } from './useAuth';
import {
  browserEditorRuntime,
  type ResumeEditorActions,
} from './useResumeEditor';

interface Kinded {
  readonly kind: string;
}

interface Inputs<T extends Kinded> {
  readonly inspector: Ref<T>;
  readonly narrowRegion: Ref<'editor' | 'preview'>;
  readonly saveState: Readonly<Ref<SaveState>>;
  readonly record: () => ResumeRecord;
  readonly sessionCheckFailing: () => boolean;
  readonly actions: ResumeEditorActions;
}

const railActions: Readonly<Record<string, string>> = {
  personal: 'open-document',
  section: 'open-document',
  structure: 'open-structure',
  customization: 'open-design',
  templates: 'open-templates',
  photo: 'open-photo',
  history: 'open-history',
};

function isWideLayout(): boolean {
  return typeof window.matchMedia === 'function'
    ? window.matchMedia('(min-width: 72rem)').matches
    : window.innerWidth >= 72 * 16;
}

function focusSelector(selector: string): void {
  globalThis.document.querySelector<HTMLElement>(selector)?.focus();
}

export function useEditorHistory<T extends Kinded>(inputs: Inputs<T>) {
  const { inspector, narrowRegion } = inputs;
  const previous = ref(inspector.value) as Ref<T>;
  const preview = ref<{ focusTitle: () => void } | null>(null);
  const history: HistoryController = createHistoryController({
    resumeId: inputs.record().accepted.metadata.id,
    store: useResumeStore(),
    auth: useAuth(),
    runtime: browserEditorRuntime,
    refresh: () => inputs.actions.refresh(),
  });

  const restoreBlocked = computed(
    () =>
      (inputs.saveState.value !== 'saved' && inputs.saveState.value !== 'idle')
      || inputs.record().conflicts.length > 0
      || inputs.sessionCheckFailing(),
  );
  const previewingVersion = computed(
    () => inspector.value.kind === 'history'
      && history.selected.value !== 'current',
  );

  function openHistory(): void {
    if (inspector.value.kind !== 'history') previous.value = inspector.value;
    inspector.value = { kind: 'history' } as T;
  }

  async function leaveHistory(): Promise<void> {
    const target = previous.value;
    inspector.value = target;
    narrowRegion.value = 'editor';
    await nextTick();
    focusSelector(`[data-action="${railActions[target.kind]}"]`);
  }

  async function escapeHistory(): Promise<void> {
    const shown = history.selected.value;
    await history.select('current');
    narrowRegion.value = 'editor';
    await nextTick();
    focusSelector(`[data-version-id="${shown}"]`);
  }

  watch(() => history.selected.value, async (id) => {
    if (inspector.value.kind !== 'history' || id === 'current') return;
    if (isWideLayout()) return;
    narrowRegion.value = 'preview';
    await nextTick();
    preview.value?.focusTitle();
  });

  watch(() => history.banner.value, (banner) => {
    if (banner !== null) narrowRegion.value = 'editor';
  });

  return {
    history,
    preview,
    restoreBlocked,
    previewingVersion,
    openHistory,
    leaveHistory,
    escapeHistory,
  };
}
