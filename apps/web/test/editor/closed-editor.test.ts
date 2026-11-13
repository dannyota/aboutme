import { TEMPLATES } from '@aboutme/schema/templates';
import { createPinia, setActivePinia } from 'pinia';
import { computed, ref } from 'vue';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

// eslint-disable-next-line max-len
import { createResumeEditorActions } from '../../app/composables/useResumeEditor';
import { createMutationCoordinator } from '../../app/editor/coordinator';
import type { AtomicCommandIntent } from '../../app/editor/commands';
import {
  captureTemplateGroup,
  projectTemplateTarget,
} from '../../app/editor/templateGroup';
import { useResumeStore } from '../../app/stores/resumes';
import {
  fakeServer,
  fullName,
  headline,
  startingResume,
  type FakeResumeServer,
} from './fake-resume-server';

// Work still in flight when the editor closes: whatever comes back belongs to
// the closed editor and must neither send more nor change the resume the
// next editor opened.

const ID = 'resume-1';

function session() {
  setActivePinia(createPinia());
  const initial = startingResume();
  const server = fakeServer(initial);
  const store = useResumeStore();
  store.initialize(initial);
  return { server, store };
}

type Session = ReturnType<typeof session>;

/** An editor page's actions over the shared store and server. */
function openEditor({ server, store }: Session) {
  const csrfToken = ref<string | null>('csrf-1');
  let finishRefresh = (): void => {};
  const auth = {
    user: computed(() => ({ id: 'owner-1', hasPassword: true })),
    csrfToken: computed(() => csrfToken.value),
    authState: computed(() => 'authenticated'),
    identities: computed(() => []),
    refresh: vi.fn(() => new Promise<void>((resolve) => {
      finishRefresh = () => {
        csrfToken.value = 'csrf-2';
        resolve();
      };
    })),
    mutate: vi.fn(),
  };
  let ids = 0;
  const runtime = {
    nowEpochMs: () => Date.now(),
    uuid: () => `${crypto.randomUUID()}-${(ids += 1)}`,
    delay: (ms: number) =>
      new Promise<void>((resolve) => setTimeout(resolve, ms)),
  };
  const coordinator = createMutationCoordinator({
    api: server.api as never,
    store,
    auth: auth as never,
    runtime,
  });
  const actions = createResumeEditorActions({
    resumeId: ID,
    store,
    coordinator,
    auth: auth as never,
    runtime,
  });
  return {
    actions,
    /** The session's CSRF token is gone, so the next send refreshes it. */
    dropToken: () => {
      csrfToken.value = null;
    },
    finishRefresh: () => finishRefresh(),
    edit: (intent: AtomicCommandIntent): void => {
      expect(actions.edit(intent).kind).toBe('enqueued');
    },
  };
}

/** Another editor page opens on the resume from a fresh complete read. */
function reopen(context: Session) {
  context.store.initialize(context.server.stored);
  return openEditor(context);
}

function headlineElsewhere(server: FakeResumeServer, value: string): void {
  server.otherWriter((document) => ({
    ...document,
    personalDetails: { ...document.personalDetails, headline: value },
  }));
}

/** The resume the reopened editor shows is the server's, untouched. */
function expectFreshRecord(context: Session): void {
  expect(context.store.recordFor(ID)).toMatchObject({
    accepted: context.server.stored,
    pending: [],
    attempt: null,
    conflicts: [],
    completeReadRequired: false,
  });
  expect(context.store.saveStateFor(ID)).toBe('saved');
}

const TIMINGS = [
  ['after the editor closes', false],
  ['after the resume is opened again', true],
] as const;

beforeEach(() => {
  vi.useFakeTimers();
  vi.setSystemTime(0);
});

afterEach(() => {
  vi.useRealTimers();
});

describe('work in flight when the editor closes', () => {
  it.each(TIMINGS)(
    'sends nothing once a session refresh finishes %s',
    async (_timing, reopenFirst) => {
      const context = session();
      const first = openEditor(context);
      first.dropToken();
      first.edit(headline('Staff engineer'));
      await vi.advanceTimersByTimeAsync(1_000);
      expect(context.server.sent).toEqual([]);

      first.actions.stop();
      const second = reopenFirst ? reopen(context) : undefined;
      first.finishRefresh();
      await vi.advanceTimersByTimeAsync(60_000);
      if (second === undefined) reopen(context);

      expect(context.server.sent).toEqual([]);
      expectFreshRecord(context);
    },
  );

  it.each(TIMINGS)(
    'sends no rebase for a 412 that arrives %s',
    async (_timing, reopenFirst) => {
      const context = session();
      const first = openEditor(context);
      first.edit(headline('Staff engineer'));
      context.server.otherWriter((document) => document);
      const release = context.server.holdNextResponse();
      await vi.advanceTimersByTimeAsync(1_000);
      expect(context.server.sent.map(({ result }) => result))
        .toEqual(['stale']);

      first.actions.stop();
      const second = reopenFirst ? reopen(context) : undefined;
      release();
      await vi.advanceTimersByTimeAsync(60_000);
      if (second === undefined) reopen(context);

      expect(context.server.sent.map(({ result }) => result))
        .toEqual(['stale']);
      expectFreshRecord(context);
    },
  );

  it.each(TIMINGS)(
    'leaves the reopened resume alone when an older success arrives %s',
    async (_timing, reopenFirst) => {
      const context = session();
      const first = openEditor(context);
      first.edit(headline('Staff engineer'));
      first.edit(fullName('Ada King'));
      const release = context.server.holdNextResponse();
      await vi.advanceTimersByTimeAsync(1_000);
      expect(context.server.sent.map(({ result }) => result))
        .toEqual(['complete']);
      headlineElsewhere(context.server, 'Lead engineer');

      first.actions.stop();
      const second = reopenFirst ? reopen(context) : undefined;
      release();
      await vi.advanceTimersByTimeAsync(60_000);
      if (second === undefined) reopen(context);

      expect(context.server.sent).toHaveLength(1);
      expectFreshRecord(context);
      expect(context.store.recordFor(ID)!.current.document.personalDetails)
        .toMatchObject({ headline: 'Lead engineer', fullName: 'Ada Lovelace' });
    },
  );
});

/** A template that changes the resume the reopened editor read. */
function changingTemplate(context: Session) {
  const current = context.server.stored;
  for (const preset of TEMPLATES) {
    const group = captureTemplateGroup({
      resumeId: ID,
      ownerId: 'owner-1',
      sequence: 1,
      current,
      preset,
      dependencyIds: [],
      runtime: {
        nowEpochMs: () => 0,
        uuid: () => crypto.randomUUID(),
        delay: async () => {},
      },
    });
    if (group !== null) return { preset, group, current };
  }
  throw new Error('no template changes the starting resume');
}

describe('template actions of an editor that closed', () => {
  const stopped = { kind: 'blocked', reason: 'stopped' };

  it('apply nothing to the reopened resume', () => {
    const context = session();
    const first = openEditor(context);
    first.actions.stop();
    const second = reopen(context);
    const { preset } = changingTemplate(context);

    expect(first.actions.applyTemplate(preset)).toEqual(stopped);
    expectFreshRecord(context);
    expect(second.actions.applyTemplate(preset).kind).toBe('enqueued');
  });

  it('undo nothing the reopened editor applied', () => {
    const context = session();
    const first = openEditor(context);
    first.actions.stop();
    const second = reopen(context);
    const { group, current } = changingTemplate(context);
    const complete = {
      kind: 'complete' as const,
      finalRevision: current.revision,
      undo: {
        groupId: group.id,
        finalRevision: current.revision,
        preApplyTarget: group.intended,
        finalTarget: projectTemplateTarget(current),
        contentContext: group.contentContext,
      },
    };
    context.store.setTemplateState(ID, complete);

    expect(first.actions.undoTemplate()).toEqual(stopped);
    expect(context.store.recordFor(ID)).toMatchObject({
      pending: [], templateState: complete,
    });
    expect(second.actions.undoTemplate().kind).toBe('enqueue');
  });

  it('recover nothing of a template the reopened editor left partial', () => {
    const context = session();
    const first = openEditor(context);
    first.actions.stop();
    const second = reopen(context);
    const { group, current } = changingTemplate(context);
    context.store.enqueue(ID, group);
    const partial = {
      kind: 'partial' as const,
      accepted: current,
      nextChild: 0 as const,
      reason: 'child-failed' as const,
    };
    context.store.setTemplateState(ID, partial);

    expect(first.actions.recoverTemplate('keep-partial')).toEqual(stopped);
    expect(context.store.recordFor(ID)!.pending.map(({ id }) => id))
      .toEqual([group.id]);
    expect(context.store.recordFor(ID)!.templateState).toEqual(partial);
    expect(second.actions.recoverTemplate('keep-partial'))
      .toEqual({ kind: 'keep-partial' });
  });
});
