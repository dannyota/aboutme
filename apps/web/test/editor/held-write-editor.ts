import { createPinia, setActivePinia } from 'pinia';
import { computed } from 'vue';
import { vi } from 'vitest';

// eslint-disable-next-line max-len
import { createResumeEditorActions } from '../../app/composables/useResumeEditor';
import { createMutationCoordinator } from '../../app/editor/coordinator';
import type { AtomicCommandIntent } from '../../app/editor/commands';
import { useResumeStore } from '../../app/stores/resumes';
import { fakeServer, startingResume } from './fake-resume-server';

/**
 * One open editor on the fake server: the real store, coordinator and
 * actions, driven by fake timers the caller installs.
 */
export function openEditor() {
  setActivePinia(createPinia());
  const initial = startingResume();
  const server = fakeServer(initial);
  const store = useResumeStore();
  store.initialize(initial);
  const auth = {
    user: computed(() => ({ id: 'owner-1' })),
    csrfToken: computed(() => 'csrf-1'),
    authState: computed(() => 'authenticated'),
    refresh: vi.fn(async () => {}),
  };
  let ids = 0;
  const runtime = {
    nowEpochMs: () => Date.now(),
    uuid: () => `id-${(ids += 1)}`,
    delay: (ms: number) =>
      new Promise<void>((resolve) => setTimeout(resolve, ms)),
  };
  const id = initial.metadata.id;
  const coordinator = createMutationCoordinator({
    api: server.api as never,
    store,
    auth: auth as never,
    runtime,
  });
  const actions = createResumeEditorActions({
    resumeId: id,
    store,
    coordinator,
    auth: auth as never,
    runtime,
  });
  const edit = (intent: AtomicCommandIntent): string => {
    const result = actions.edit(intent);
    if (result.kind !== 'enqueued') throw new Error(result.reason);
    return result.command.id;
  };
  return {
    actions,
    auth,
    edit,
    id,
    initial,
    server,
    store,
    record: () => store.recordFor(id)!,
    /** Two minutes of the editor's own timers, with no flush from the test. */
    idle: () => vi.advanceTimersByTimeAsync(120_000),
  };
}

export type OpenEditor = ReturnType<typeof openEditor>;
