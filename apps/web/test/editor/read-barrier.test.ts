import type { Resume } from '@aboutme/schema';
import { createPinia, setActivePinia } from 'pinia';
import { computed } from 'vue';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

// eslint-disable-next-line max-len
import { createResumeEditorActions } from '../../app/composables/useResumeEditor';
import { hasUnsafeWork } from '../../app/composables/useUnsavedNavigationGuard';
import type {
  AttemptResult,
  FrozenAttempt,
  ResumeReadResult,
} from '../../app/editor/attempt';
import type { AtomicCommandIntent } from '../../app/editor/commands';
import { createMutationCoordinator } from '../../app/editor/coordinator';
import { createPublishController } from '../../app/editor/publishController';
import type { PublishApi } from '../../app/editor/publishApi';
import { parentETag, parseRevision } from '../../app/editor/revision';
import type { AcceptedResume } from '../../app/editor/types';
import { useResumeStore } from '../../app/stores/resumes';
import { acceptedFixture } from './fixture';

type ReadFailure = Exclude<ResumeReadResult, { kind: 'complete' }>;

const FAILURES = [
  ['a network error', { kind: 'failed', reason: 'network' }],
  ['a server error', { kind: 'failed', reason: 'response-invalid' }],
  ['a lost session', { kind: 'session-lost' }],
] as const satisfies readonly (readonly [string, ReadFailure])[];

const headline = (value: string): AtomicCommandIntent => ({
  kind: 'personalField',
  path: 'headline',
  value: { present: true, value },
});

const fullName = (value: string): AtomicCommandIntent => ({
  kind: 'personalField',
  path: 'fullName',
  value: { present: true, value },
});

function startingResume(): AcceptedResume {
  const base = acceptedFixture();
  return {
    ...base,
    document: {
      ...base.document,
      personalDetails: {
        ...base.document.personalDetails,
        photo: { key: 'photo-1' },
      },
      content: {
        work: {
          sectionType: 'work',
          entries: [
            { id: 'entry-1', jobTitle: 'Engineer' },
            { id: 'entry-2', jobTitle: 'Lead' },
          ],
        },
      },
      customization: {
        ...base.document.customization,
        layout: {
          ...base.document.customization.layout,
          sections: { main: ['work'], sidebar: [] },
        },
      },
    },
  };
}

/**
 * The owner API over one stored resume. It checks If-Match as the server
 * does, answers a stale write with the stored document and revision, and
 * applies the writes these scenarios send.
 */
function fakeServer(initial: AcceptedResume) {
  let stored = structuredClone(initial);
  const failedReads: ReadFailure[] = [];
  const sent: { attempt: FrozenAttempt; result: AttemptResult['kind'] }[]
    = [];
  const readTimes: number[] = [];

  const advance = (document: Resume): void => {
    stored = {
      ...stored,
      document: structuredClone(document),
      revision: parseRevision(String(Number(stored.revision) + 1)),
    };
  };
  const apply = (attempt: FrozenAttempt): AttemptResult => {
    const document = stored.document;
    switch (attempt.operation) {
      case 'updateResumePersonalDetails': {
        if (attempt.payload.kind !== 'json') throw new Error('no body');
        const body = JSON.parse(attempt.payload.utf8);
        const photo = document.personalDetails.photo;
        advance({
          ...document,
          personalDetails: photo === undefined ? body : { ...body, photo },
        });
        return { kind: 'complete', status: 200, accepted: copy() };
      }
      case 'deleteResumeEntry': {
        const entryId = decodeURIComponent(attempt.url.split('/').at(-1)!);
        const work = document.content.work!;
        advance({
          ...document,
          content: {
            work: {
              ...work,
              entries: work.entries.filter((entry) => entry.id !== entryId),
            },
          },
        });
        return childAck('entry');
      }
      case 'linkResumeProject':
        return { kind: 'rejected', code: 'project_not_found' };
      case 'deleteResumePhoto': {
        const { photo: _photo, ...personalDetails } = document.personalDetails;
        advance({ ...document, personalDetails });
        return childAck('photo');
      }
      default:
        throw new Error(`unexpected operation ${attempt.operation}`);
    }
  };
  const childAck = (scope: 'entry' | 'photo'): AttemptResult => ({
    kind: 'child-ack',
    status: 204,
    scope,
    etag: parentETag(stored.revision),
  });
  const copy = (): AcceptedResume => structuredClone(stored);

  const api = {
    dispatch: vi.fn(async (attempt: FrozenAttempt): Promise<AttemptResult> => {
      const result: AttemptResult = attempt.ifMatch === parentETag(
        stored.revision,
      )
        ? apply(attempt)
        : {
            kind: 'stale',
            status: 412,
            winner: {
              document: structuredClone(stored.document),
              revision: stored.revision,
            },
          };
      sent.push({ attempt, result: result.kind });
      return result;
    }),
    read: vi.fn(async (): Promise<ResumeReadResult> => {
      readTimes.push(Date.now());
      return failedReads.shift() ?? { kind: 'complete', accepted: copy() };
    }),
  };

  return {
    api,
    sent,
    get stored() {
      return copy();
    },
    get reads() {
      return readTimes.length;
    },
    /** Milliseconds between consecutive reads. */
    get readGaps() {
      return readTimes.slice(1).map((time, index) => time - readTimes[index]!);
    },
    failNextRead(failure: ReadFailure): void {
      failedReads.push(failure);
    },
    /** Another tab or agent changes the resume. */
    otherWriter(change: (document: Resume) => Resume): void {
      advance(change(structuredClone(stored.document)));
    },
    /** Attempts sent for one command, with what each got back. */
    sentFor(commandId: string): AttemptResult['kind'][] {
      return sent
        .filter(({ attempt }) => attempt.id === commandId)
        .map(({ result }) => result);
    },
  };
}

function editor(initial = startingResume()) {
  setActivePinia(createPinia());
  const server = fakeServer(initial);
  const store = useResumeStore();
  store.initialize(initial);
  return openEditor(initial.metadata.id, server, store);
}

/** An editor on a resume the store already holds, as the page builds it. */
function openEditor(
  id: string,
  server: ReturnType<typeof fakeServer>,
  store: ReturnType<typeof useResumeStore>,
) {
  const auth = {
    user: computed(() => ({ id: 'owner-1', hasPassword: true })),
    csrfToken: computed(() => 'csrf-1'),
    authState: computed(() => 'authenticated'),
    identities: computed(() => []),
    refresh: vi.fn(async () => {}),
    mutate: vi.fn(),
  };
  const runtime = {
    nowEpochMs: () => Date.now(),
    uuid: () => `id-${(ids += 1)}`,
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
  const record = () => store.recordFor(id)!;
  const conflictId = (): string => record().conflicts[0]!.id;
  /** Lets the editor's own timers run, with no flush from the test. */
  const idle = async (): Promise<void> => {
    for (let second = 0; second < 30; second += 1) {
      await vi.advanceTimersByTimeAsync(1_000);
    }
  };
  /** The person signs in again and resumes, as the session dialog offers. */
  const reauthenticate = async (): Promise<void> => {
    expect(store.saveStateFor(id)).toBe('session-lost');
    await actions.resumeAfterAuth();
  };
  /** The person comes back to this resume after its editor closed. */
  const reopen = () => openEditor(id, server, store);
  return {
    actions,
    auth,
    conflictId,
    coordinator,
    edit,
    id,
    idle,
    reauthenticate,
    record,
    reopen,
    runtime,
    server,
    store,
  };
}

type Editor = ReturnType<typeof editor>;

/** IDs stay unique across the editors one test opens on a resume. */
let ids = 0;

/** The later edit reached the server once, on a current revision. */
async function expectLaterEditSent(context: Editor): Promise<void> {
  const later = context.edit(fullName('Ada Byron'));
  expect(hasUnsafeWork(context.record())).toBe(true);
  await context.idle();
  expect(context.server.sentFor(later)).toEqual(['complete']);
  expect(context.server.stored.document.personalDetails.fullName).toBe(
    'Ada Byron',
  );
  expect(context.record()).toMatchObject({
    pending: [],
    attempt: null,
    conflicts: [],
    completeReadRequired: false,
    sessionLost: false,
  });
  expect(context.store.saveStateFor(context.id)).toBe('saved');
  expect(hasUnsafeWork(context.record())).toBe(false);
}

/** Recovers from a failed read: the editor's timers, or signing in again. */
async function recover(context: Editor, failure: ReadFailure): Promise<void> {
  if (failure.kind === 'session-lost') await context.reauthenticate();
  await context.idle();
}

beforeEach(() => {
  ids = 0;
  vi.useFakeTimers();
  vi.setSystemTime(0);
});

afterEach(() => {
  vi.useRealTimers();
});

describe('a satisfied stale write', () => {
  it('sends the edit typed after it', async () => {
    const context = editor();
    const mine = context.edit(headline('Staff engineer'));
    context.server.otherWriter((document) => ({
      ...document,
      personalDetails: {
        ...document.personalDetails,
        headline: 'Staff engineer',
      },
    }));

    await context.idle();

    expect(context.server.sentFor(mine)).toEqual(['stale']);
    await expectLaterEditSent(context);
  });

  it('settles saved with nothing else queued', async () => {
    const context = editor();
    context.edit(headline('Staff engineer'));
    context.server.otherWriter((document) => ({
      ...document,
      personalDetails: {
        ...document.personalDetails,
        headline: 'Staff engineer',
      },
    }));

    await context.idle();

    expect(context.record().completeReadRequired).toBe(false);
    expect(context.store.saveStateFor(context.id)).toBe('saved');
    expect(hasUnsafeWork(context.record())).toBe(false);
  });

  it.each(FAILURES)(
    'retries a completing read that failed with %s',
    async (_name, failure) => {
      const context = editor();
      context.edit(headline('Staff engineer'));
      context.server.otherWriter((document) => ({
        ...document,
        personalDetails: {
          ...document.personalDetails,
          headline: 'Staff engineer',
        },
      }));
      context.server.failNextRead(failure);

      await context.idle();
      await recover(context, failure);

      await expectLaterEditSent(context);
    },
  );
});

describe('the completing read', () => {
  function satisfiedStale(): Editor {
    const context = editor();
    context.edit(headline('Staff engineer'));
    context.server.otherWriter((document) => ({
      ...document,
      personalDetails: {
        ...document.personalDetails,
        headline: 'Staff engineer',
      },
    }));
    return context;
  }

  it('waits for Retry-After before retrying when rate limited', async () => {
    const context = satisfiedStale();
    context.server.failNextRead({ kind: 'rate-limited', retryAfterMs: 5_000 });

    await vi.advanceTimersByTimeAsync(1_000);
    expect(context.server.reads).toBe(1);
    expect(context.store.saveStateFor(context.id)).toBe('dirty');
    await vi.advanceTimersByTimeAsync(4_999);
    expect(context.server.reads).toBe(1);
    await vi.advanceTimersByTimeAsync(1);

    expect(context.server.reads).toBe(2);
    await expectLaterEditSent(context);
  });

  it('doubles its retry delay to 30 s and starts at 1 s again after a '
    + 'success', async () => {
    const context = satisfiedStale();
    for (let failure = 0; failure < 7; failure += 1) {
      context.server.failNextRead({ kind: 'failed', reason: 'network' });
    }

    await vi.advanceTimersByTimeAsync(120_000);

    expect(context.server.readGaps).toEqual([
      1_000, 2_000, 4_000, 8_000, 16_000, 30_000, 30_000,
    ]);
    expect(context.store.saveStateFor(context.id)).toBe('saved');
    context.edit(headline('Lead engineer'));
    context.server.otherWriter((document) => ({
      ...document,
      personalDetails: {
        ...document.personalDetails,
        headline: 'Lead engineer',
      },
    }));
    context.server.failNextRead({ kind: 'failed', reason: 'network' });
    await context.idle();

    expect(context.server.readGaps.at(-1)).toBe(1_000);
    await expectLaterEditSent(context);
  });

  it('waits at least its current delay when Retry-After is shorter',
    async () => {
      const context = satisfiedStale();
      context.server.failNextRead({ kind: 'failed', reason: 'network' });
      context.server.failNextRead({ kind: 'failed', reason: 'network' });
      context.server.failNextRead({
        kind: 'rate-limited',
        retryAfterMs: 1_000,
      });

      await context.idle();

      expect(context.server.readGaps).toEqual([1_000, 2_000, 4_000]);
      await expectLaterEditSent(context);
    });

  it('stops retrying when the resume no longer exists', async () => {
    const context = satisfiedStale();
    context.server.failNextRead({ kind: 'unavailable' });

    await vi.advanceTimersByTimeAsync(120_000);

    expect(context.server.reads).toBe(1);
    expect(context.record().completeReadRequired).toBe(true);
    expect(context.store.saveStateFor(context.id)).toBe('dirty');
  });

  it('issues no read after its editor closes, and completes when the '
    + 'resume is opened again', async () => {
    const context = satisfiedStale();
    context.server.failNextRead({ kind: 'failed', reason: 'network' });
    await vi.advanceTimersByTimeAsync(1_000);
    expect(context.server.reads).toBe(1);

    context.actions.stop();
    await vi.advanceTimersByTimeAsync(120_000);

    expect(context.server.reads).toBe(1);
    expect(hasUnsafeWork(context.record())).toBe(true);
    const returned = context.reopen();
    await expectLaterEditSent(returned);
    expect(context.server.reads).toBe(2);
  });

  it('is completed by a realtime read at the held revision', async () => {
    const context = satisfiedStale();
    context.server.failNextRead({ kind: 'failed', reason: 'network' });
    await vi.advanceTimersByTimeAsync(1_000);
    expect(context.record().completeReadRequired).toBe(true);

    await context.actions.refresh();

    expect(context.record().completeReadRequired).toBe(false);
    expect(context.store.saveStateFor(context.id)).toBe('saved');
    expect(context.server.reads).toBe(2);
  });

  it('follows a refused project link and is retried when it fails',
    async () => {
      const context = editor();
      const link = context.edit({ kind: 'projectLink', projectId: 'p-1' });
      context.server.failNextRead({ kind: 'failed', reason: 'network' });

      await context.idle();

      expect(context.server.sentFor(link)).toEqual(['rejected']);
      expect(context.server.reads).toBe(2);
      await expectLaterEditSent(context);
    });
});

function conflicted(): Editor {
  const context = editor();
  context.edit(headline('Mine'));
  context.server.otherWriter((document) => ({
    ...document,
    personalDetails: { ...document.personalDetails, headline: 'Theirs' },
  }));
  return context;
}

describe('a stale write that conflicts', () => {
  it('sends the edit typed after accept latest', async () => {
    const context = conflicted();
    await context.idle();
    expect(context.store.saveStateFor(context.id)).toBe('conflict');

    await context.actions.acceptLatest(context.conflictId());
    await context.idle();

    expect(context.server.stored.document.personalDetails.headline).toBe(
      'Theirs',
    );
    await expectLaterEditSent(context);
  });

  it('sends the edit typed after apply mine', async () => {
    const context = conflicted();
    await context.idle();

    await context.actions.applyMine(context.conflictId(), { kind: 'field' });
    await context.idle();

    expect(context.server.stored.document.personalDetails.headline).toBe(
      'Mine',
    );
    await expectLaterEditSent(context);
  });

  it.each(FAILURES)(
    'retries the accept latest read that failed with %s',
    async (_name, failure) => {
      const context = conflicted();
      await context.idle();
      context.server.failNextRead(failure);

      await context.actions.acceptLatest(context.conflictId());
      await recover(context, failure);

      expect(context.record().conflicts).toEqual([]);
      await expectLaterEditSent(context);
    },
  );

  it.each(FAILURES)(
    'keeps the conflict when the apply mine read fails with %s, '
    + 'then applies it',
    async (_name, failure) => {
      const context = conflicted();
      await context.idle();
      context.server.failNextRead(failure);

      await context.actions.applyMine(context.conflictId(), { kind: 'field' });
      expect(context.record()).toMatchObject({
        conflicts: [{ command: { kind: 'personalField' } }],
        sessionLost: failure.kind === 'session-lost',
      });
      await recover(context, failure);
      await context.actions.applyMine(context.conflictId(), { kind: 'field' });
      await context.idle();

      expect(context.server.stored.document.personalDetails.headline).toBe(
        'Mine',
      );
      await expectLaterEditSent(context);
    },
  );
});

const DELETES = [
  [
    'an entry',
    { kind: 'entryDelete', sectionKey: 'work', entryId: 'entry-1' },
  ],
  ['a photo', { kind: 'photoDelete' }],
] as const satisfies readonly (readonly [string, AtomicCommandIntent])[];

describe('a delete acknowledged without a body', () => {
  it.each(DELETES)('sends the edit typed after %s', async (_name, intent) => {
    const context = editor();
    const deleted = context.edit(intent);

    await context.idle();

    expect(context.server.sentFor(deleted)).toEqual(['child-ack']);
    await expectLaterEditSent(context);
  });

  it.each(
    DELETES.flatMap(([name, intent]) =>
      FAILURES.map(
        ([reason, failure]) => [name, reason, intent, failure] as const,
      ),
    ),
  )(
    'retries the read after %s when it fails with %s',
    async (_name, _reason, intent, failure) => {
      const context = editor();
      context.edit(intent);
      context.server.failNextRead(failure);

      await context.idle();
      await recover(context, failure);

      await expectLaterEditSent(context);
    },
  );
});

function publishing(context: Editor) {
  const api: PublishApi = {
    dispatch: vi.fn(async () => ({
      kind: 'stale' as const,
      winner: {
        document: context.server.stored.document,
        revision: context.server.stored.revision,
      },
    })),
  };
  return createPublishController({
    resumeId: context.id,
    store: context.store,
    coordinator: context.coordinator,
    auth: context.auth as never,
    api,
    runtime: context.runtime,
    providerLogin: false,
  });
}

const publishCommand = {
  slug: 'ada-lovelace',
  live: true,
  downloadEnabled: false,
  seoGeoEnabled: false,
};

describe('a publish answered with a newer revision', () => {
  it('sends the edit typed after it', async () => {
    const context = editor();
    context.server.otherWriter((document) => ({
      ...document,
      personalDetails: { ...document.personalDetails, headline: 'Theirs' },
    }));

    await publishing(context).submit(publishCommand);
    await context.idle();

    expect(context.record().accepted.revision).toBe(parseRevision('2'));
    await expectLaterEditSent(context);
  });

  it.each(FAILURES)(
    'retries its completing read that failed with %s',
    async (_name, failure) => {
      const context = editor();
      context.server.otherWriter((document) => ({
        ...document,
        personalDetails: { ...document.personalDetails, headline: 'Theirs' },
      }));
      context.server.failNextRead(failure);

      await expect(
        publishing(context).submit(publishCommand),
      ).resolves.toMatchObject({ kind: 'blocked' });
      await recover(context, failure);

      await expectLaterEditSent(context);
    },
  );
});

describe('a stale write whose winner changed only the revision', () => {
  it('rebases one queued edit without a conflict', async () => {
    const context = editor();
    const mine = context.edit(headline('Staff engineer'));
    context.server.otherWriter((document) => document);

    await context.idle();

    expect(context.server.sentFor(mine)).toEqual(['stale', 'complete']);
    expect(context.server.stored.document.personalDetails.headline).toBe(
      'Staff engineer',
    );
    expect(context.store.saveStateFor(context.id)).toBe('saved');
    await expectLaterEditSent(context);
  });

  it('rebases the first of several queued edits and sends the rest once',
    async () => {
      const context = editor();
      const first = context.edit(headline('Staff engineer'));
      const second = context.edit(fullName('Ada King'));
      const third = context.edit(headline('Principal engineer'));
      context.server.otherWriter((document) => document);

      await context.idle();

      expect(context.server.sentFor(first)).toEqual(['stale', 'complete']);
      expect(context.server.sentFor(second)).toEqual(['complete']);
      expect(context.server.sentFor(third)).toEqual(['complete']);
      expect(context.server.stored.document.personalDetails).toMatchObject({
        fullName: 'Ada King',
        headline: 'Principal engineer',
      });
      expect(context.record().conflicts).toEqual([]);
      await expectLaterEditSent(context);
    });

  it.each(FAILURES)(
    'needs no completing read, so a read failing with %s changes nothing',
    async (_name, failure) => {
      const context = editor();
      const mine = context.edit(headline('Staff engineer'));
      context.server.otherWriter((document) => document);
      context.server.failNextRead(failure);

      await context.idle();

      expect(context.server.sentFor(mine)).toEqual(['stale', 'complete']);
      expect(context.server.reads).toBe(0);
      await expectLaterEditSent(context);
    },
  );
});
