import { createPinia, setActivePinia } from 'pinia';
import { computed } from 'vue';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

// eslint-disable-next-line max-len
import { createResumeEditorActions } from '../../app/composables/useResumeEditor';
import { createMutationCoordinator } from '../../app/editor/coordinator';
import type { AtomicCommandIntent } from '../../app/editor/commands';
import { parseRevision } from '../../app/editor/revision';
import { useResumeStore } from '../../app/stores/resumes';
import {
  fakeServer,
  fullName,
  headline,
  startingResume,
} from './fake-resume-server';

// A write the queue holds because its outcome is unknown or because it keeps
// meeting newer revisions: how it is retried, when the person is told, and
// that the edits queued behind it still reach the server, in order.

function editor() {
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

beforeEach(() => {
  vi.useFakeTimers();
  vi.setSystemTime(0);
});

afterEach(() => {
  vi.useRealTimers();
});

describe('a write whose outcome stays unknown after its replay', () => {
  it.each([
    ['never reached the server', false],
    ['was applied but its responses were lost', true],
  ])(
    'is retried with its original key when it %s, and the edits behind it '
    + 'are sent',
    async (_case, applied) => {
      const context = editor();
      const mine = context.edit(headline('Staff engineer'));
      const later = context.edit(fullName('Ada King'));
      context.server.loseNextResponse({ applied });
      context.server.loseNextResponse({ applied });

      await context.idle();
      await context.actions.refresh();
      await context.idle();

      const keys = context.server.sent
        .filter(({ attempt }) => attempt.id === mine)
        .map(({ attempt }) => attempt.idempotencyKey);
      expect(new Set(keys).size).toBe(1);
      expect(context.server.sentFor(later)).toEqual(['complete']);
      expect(context.server.stored).toMatchObject({
        revision: parseRevision('3'),
        document: {
          personalDetails: { headline: 'Staff engineer', fullName: 'Ada King' },
        },
      });
      expect(context.record()).toMatchObject({ attempt: null, pending: [] });
      expect(context.store.saveStateFor(context.id)).toBe('saved');
    },
  );
});

describe('a write that meets a newer revision twice', () => {
  it('rebases again when a linked project is edited twice in quick '
    + 'succession, and the edits behind it are sent', async () => {
    const context = editor();
    const mine = context.edit(headline('Staff engineer'));
    const later = context.edit(fullName('Ada King'));
    // The server bumps the revision without a document change each time a
    // linked library project is edited; two edits land around this write.
    context.server.beforeNextWrite(() => {
      context.server.otherWriter((document) => document);
    });
    context.server.beforeNextWrite(() => {
      context.server.otherWriter((document) => document);
    });

    await context.idle();
    await context.actions.refresh();
    await context.idle();

    expect(context.server.sentFor(mine).at(-1)).toBe('complete');
    expect(context.server.sentFor(later)).toEqual(['complete']);
    expect(context.record()).toMatchObject({
      attempt: null,
      pending: [],
      conflicts: [],
    });
    expect(context.store.saveStateFor(context.id)).toBe('saved');
  });
});

describe('the retry of a write whose outcome is unknown', () => {
  it('follows the retry schedule without a busy loop', async () => {
    const context = editor();
    context.server.loseEveryResponse({ applied: false });
    const mine = context.edit(headline('Staff engineer'));

    await vi.advanceTimersByTimeAsync(121_000);

    expect(context.server.sendGapsFor(mine)).toEqual([
      250, 1_000, 2_000, 4_000, 8_000, 16_000, 30_000, 30_000,
    ]);
    const keys = context.server.sent
      .map(({ attempt }) => attempt.idempotencyKey);
    expect(new Set(keys).size).toBe(1);
  });

  it('sends nothing else while the write is unknown, then the rest in order',
    async () => {
      const context = editor();
      const first = context.edit(headline('Staff engineer'));
      const second = context.edit(fullName('Ada King'));
      const third = context.edit(headline('Principal engineer'));
      for (let lost = 0; lost < 4; lost += 1) {
        context.server.loseNextResponse({ applied: false });
      }

      await context.idle();

      const order = context.server.sent.map(({ attempt }) => attempt.id);
      expect(order).toEqual([
        first, first, first, first, first, second, third,
      ]);
      expect(context.server.sent.slice(0, 5)
        .map(({ attempt }) => attempt.idempotencyKey)
        .every((key, _index, keys) => key === keys[0])).toBe(true);
      expect(context.server.stored.document.personalDetails).toMatchObject({
        headline: 'Principal engineer',
        fullName: 'Ada King',
      });
      expect(context.store.saveStateFor(context.id)).toBe('saved');
    });

  it('raises the alert after exactly a minute and clears it on success',
    async () => {
      const context = editor();
      context.server.loseEveryResponse({ applied: false });
      context.edit(headline('Staff engineer'));
      // The first send goes after the one-second debounce and fails at once.
      await vi.advanceTimersByTimeAsync(1_000);
      expect(context.store.saveStateFor(context.id)).toBe('retrying');

      await vi.advanceTimersByTimeAsync(59_999);
      expect(context.record().attempt).not.toHaveProperty('alert');
      await vi.advanceTimersByTimeAsync(1);
      expect(context.record().attempt).toMatchObject({
        kind: 'unknown',
        alert: true,
      });
      expect(context.store.saveStateFor(context.id)).toBe('offline');

      context.server.loseEveryResponse(undefined);
      await vi.advanceTimersByTimeAsync(30_000);
      expect(context.record().attempt).toBeNull();
      expect(context.store.saveStateFor(context.id)).toBe('saved');
    });

  it('stops when the editor closes', async () => {
    const context = editor();
    context.server.loseEveryResponse({ applied: false });
    const mine = context.edit(headline('Staff engineer'));
    await vi.advanceTimersByTimeAsync(5_000);
    const sends = context.server.sentFor(mine).length;

    context.actions.stop();
    await vi.advanceTimersByTimeAsync(120_000);

    expect(context.server.sentFor(mine)).toHaveLength(sends);
    expect(context.record().attempt).not.toHaveProperty('alert');
  });

  it('stops when the resume is opened again', async () => {
    const context = editor();
    context.server.loseEveryResponse({ applied: false });
    const mine = context.edit(headline('Staff engineer'));
    await vi.advanceTimersByTimeAsync(5_000);
    const sends = context.server.sentFor(mine).length;

    context.store.initialize(context.server.stored);
    await vi.advanceTimersByTimeAsync(120_000);

    expect(context.server.sentFor(mine)).toHaveLength(sends);
    expect(context.record()).toMatchObject({ attempt: null, pending: [] });
  });

  it('hands over to a read and reconcile at the retry cutoff', async () => {
    const context = editor();
    context.server.loseEveryResponse({ applied: true });
    const mine = context.edit(headline('Staff engineer'));
    const later = context.edit(fullName('Ada King'));
    await vi.advanceTimersByTimeAsync(5_000);
    const sends = context.server.sentFor(mine).length;
    const reads = context.server.reads;

    vi.setSystemTime(Date.now() + 23 * 60 * 60 * 1_000);
    context.server.loseEveryResponse(undefined);
    await vi.advanceTimersByTimeAsync(31_000);

    expect(context.server.sentFor(mine)).toHaveLength(sends);
    expect(context.server.reads).toBe(reads + 1);
    expect(context.server.sentFor(later)).toEqual(['complete']);
    expect(context.store.saveStateFor(context.id)).toBe('saved');
  });

  it('restarts the queue at once when a read shows the held write landed',
    async () => {
      const context = editor();
      const mine = context.edit(headline('Staff engineer'));
      const later = context.edit(fullName('Ada King'));
      context.server.loseNextResponse({ applied: true });
      context.server.loseNextResponse({ applied: true });
      await vi.advanceTimersByTimeAsync(1_250);
      expect(context.server.sentFor(mine)).toEqual(['unknown', 'unknown']);

      await context.actions.refresh();
      await vi.advanceTimersByTimeAsync(0);

      expect(context.server.sentFor(mine)).toHaveLength(2);
      expect(context.server.sentFor(later)).toEqual(['complete']);
      expect(Date.now()).toBe(1_250);
    });
});

describe('the rebases of a write that meets newer revisions', () => {
  it('rebases three times at 1, 2 and 4 seconds, then holds until Try '
    + 'again starts a fresh cycle', async () => {
    const context = editor();
    const mine = context.edit(headline('Staff engineer'));
    const later = context.edit(fullName('Ada King'));
    for (let bump = 0; bump < 4; bump += 1) {
      context.server.beforeNextWrite(() => {
        context.server.otherWriter((document) => document);
      });
    }

    await context.idle();

    expect(context.server.sentFor(mine)).toEqual([
      'stale', 'stale', 'stale', 'stale',
    ]);
    expect(context.server.sendGapsFor(mine)).toEqual([1_000, 2_000, 4_000]);
    expect(context.record().attempt).toMatchObject({
      kind: 'failed',
      reason: 'stale-limit',
    });
    expect(context.store.saveStateFor(context.id)).toBe('error');
    expect(context.server.sentFor(later)).toEqual([]);

    const retrying = context.actions.retry(mine);
    await context.idle();
    await retrying;

    expect(context.server.sentFor(mine).at(-1)).toBe('complete');
    expect(context.server.sentFor(later)).toEqual(['complete']);
    expect(context.store.saveStateFor(context.id)).toBe('saved');
  });

  it('retries a title write whose stale read fails, on the retry schedule',
    async () => {
      const context = editor();
      const mine = context.edit({
        kind: 'metadataField', field: 'title', value: 'Ada resume',
      });
      context.server.otherWriter((document) => document);
      context.server.failNextRead({ kind: 'failed', reason: 'network' });
      context.server.failNextRead({ kind: 'failed', reason: 'network' });

      await context.idle();

      expect(context.server.readGaps.slice(0, 2)).toEqual([1_000, 2_000]);
      expect(context.server.sentFor(mine).at(-1)).toBe('complete');
      expect(context.server.stored.metadata.title).toBe('Ada resume');
      expect(context.store.saveStateFor(context.id)).toBe('saved');
    });
});
