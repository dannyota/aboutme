import { createPinia, setActivePinia } from 'pinia';
import { describe, expect, it } from 'vitest';
import { toRaw } from 'vue';

import {
  dependencyIdsForNewCommand,
  nextSequence,
  useResumeStore,
} from '../../app/stores/resumes';
import { captureCommand } from '../../app/editor/commands';
import { parentETag, parseRevision } from '../../app/editor/revision';
import type { AtomicEditorCommand } from '../../app/editor/commands';
import type { FrozenAttempt } from '../../app/editor/attempt';
import type { EditorQueueItem } from '../../app/editor/templateGroup';
import type { AcceptedResume, EditorRuntime } from '../../app/editor/types';
import { acceptedFixture } from './fixture';

const runtime: EditorRuntime = {
  nowEpochMs: () => 0,
  uuid: () => 'command-1',
  delay: async () => {},
};

function titleCommand(
  accepted: AcceptedResume,
  value: string,
  sequence = 1,
  id = 'command-1',
): AtomicEditorCommand {
  return captureCommand(
    accepted,
    {
      resumeId: accepted.metadata.id,
      ownerId: 'owner-1',
      sequence,
      dependencyIds: [],
      intent: { kind: 'metadataField', field: 'title', value },
    },
    { ...runtime, uuid: () => id },
  );
}

function entryDeleteCommand(
  accepted: AcceptedResume,
  entryId = 'entry-1',
  id = 'entry-delete-1',
): AtomicEditorCommand {
  const sectionKey = Object.keys(accepted.document.content)[0]!;
  return captureCommand(
    accepted,
    {
      resumeId: accepted.metadata.id,
      ownerId: 'owner-1',
      sequence: 1,
      dependencyIds: [],
      intent: { kind: 'entryDelete', sectionKey, entryId },
    },
    { ...runtime, uuid: () => id },
  );
}

function photoDeleteCommand(
  accepted: AcceptedResume,
  id = 'photo-delete-1',
): AtomicEditorCommand {
  return captureCommand(
    accepted,
    {
      resumeId: accepted.metadata.id,
      ownerId: 'owner-1',
      sequence: 1,
      dependencyIds: [],
      intent: { kind: 'photoDelete' },
    },
    { ...runtime, uuid: () => id },
  );
}

function photoUploadCommand(accepted: AcceptedResume): AtomicEditorCommand {
  return captureCommand(
    accepted,
    {
      resumeId: accepted.metadata.id,
      ownerId: 'owner-1',
      sequence: 1,
      dependencyIds: [],
      intent: { kind: 'photoUpload', file: new File(['photo'], 'photo.png') },
    },
    runtime,
  );
}

function withPhoto(accepted: AcceptedResume, key = 'photo-1'): AcceptedResume {
  return {
    ...accepted,
    document: {
      ...accepted.document,
      personalDetails: { ...accepted.document.personalDetails, photo: { key } },
    },
  };
}

function withEntry(accepted: AcceptedResume): AcceptedResume {
  return {
    ...accepted,
    document: {
      ...accepted.document,
      content: {
        profile: {
          sectionType: 'profile',
          entries: [{ id: 'entry-1', text: 'Existing entry' }],
        },
      },
    },
  };
}

function readyPhoto(key = 'photo-1') {
  return {
    kind: 'ready' as const,
    binding: key,
    generation: 1,
    etag: '"photo-1"' as never,
    dataUrl: 'data:image/png;base64,ready',
  };
}

function resumeDeleteCommand(
  accepted: AcceptedResume,
  id = 'resume-delete-1',
): AtomicEditorCommand {
  return captureCommand(
    accepted,
    {
      resumeId: accepted.metadata.id,
      ownerId: 'owner-1',
      sequence: 1,
      dependencyIds: [],
      intent: { kind: 'resumeDelete', confirmedTitle: accepted.metadata.title },
    },
    { ...runtime, uuid: () => id },
  );
}

function attemptFor(command: AtomicEditorCommand): FrozenAttempt {
  return {
    id: command.id,
    operation: 'updateResumeMetadata',
    url: '/api/v1/resumes/resume-1',
    method: 'PATCH',
    schemaVersion: 2,
    idempotencyKey: 'test-key',
    payload: { kind: 'empty' },
    firstDispatchAt: 0,
    retryCutoff: 1,
    automaticReplays: 0,
    staleRebases: 0,
  } as FrozenAttempt;
}

function initialized(accepted = acceptedFixture()) {
  setActivePinia(createPinia());
  const store = useResumeStore();
  store.initialize(accepted);
  return { accepted, store };
}

type ResumeStore = ReturnType<typeof useResumeStore>;
type SaveStateArrange = (store: ResumeStore, accepted: AcceptedResume) => void;

function startAttempt(
  store: ResumeStore,
  accepted: AcceptedResume,
  value: string,
): AtomicEditorCommand {
  const command = titleCommand(accepted, value);
  store.enqueue(accepted.metadata.id, command);
  store.startAttempt(
    accepted.metadata.id,
    command,
    command,
    attemptFor(command),
  );
  return command;
}

const saveStateBlockers: readonly [
  string,
  string,
  SaveStateArrange,
][] = [
  ['pending', 'dirty', (store, accepted) => {
    store.enqueue(accepted.metadata.id, titleCommand(accepted, 'Pending'));
  }],
  ['dispatching', 'saving', (store, accepted) => {
    startAttempt(store, accepted, 'Dispatching');
  }],
  ['unknown transport', 'offline', (store, accepted) => {
    const command = startAttempt(store, accepted, 'Unknown');
    store.holdAttempt(accepted.metadata.id, {
      kind: 'unknown',
      queueItem: command,
      command,
      attempt: attemptFor(command),
      reason: 'transport',
    });
  }],
  ['unknown server', 'error', (store, accepted) => {
    const command = startAttempt(store, accepted, 'Unknown');
    store.holdAttempt(accepted.metadata.id, {
      kind: 'unknown',
      queueItem: command,
      command,
      attempt: attemptFor(command),
      reason: 'server',
    });
  }],
  ['unknown cutoff', 'error', (store, accepted) => {
    const command = startAttempt(store, accepted, 'Unknown');
    store.holdAttempt(accepted.metadata.id, {
      kind: 'unknown',
      queueItem: command,
      command,
      attempt: attemptFor(command),
      reason: 'cutoff',
    });
  }],
  ['retry later', 'error', (store, accepted) => {
    const command = startAttempt(store, accepted, 'Retry');
    store.holdAttempt(accepted.metadata.id, {
      kind: 'retry-later',
      queueItem: command,
      command,
      attempt: attemptFor(command),
      reason: 'rate-limited',
      retryAfterMs: null,
    });
  }],
  ['failed', 'error', (store, accepted) => {
    const command = startAttempt(store, accepted, 'Failed');
    store.holdAttempt(accepted.metadata.id, {
      kind: 'failed',
      queueItem: command,
      command,
      attempt: attemptFor(command),
      reason: 'bad_request',
    });
  }],
  ['conflict', 'conflict', (store, accepted) => {
    store.markConflict(accepted.metadata.id, { id: 'conflict-1' } as never);
  }],
  ['partial template', 'error', (store, accepted) => {
    store.setTemplateState(accepted.metadata.id, { kind: 'partial' } as never);
  }],
  ['issues', 'error', (store, accepted) => {
    store.setIssues(accepted.metadata.id, 'command-1', [
      { path: '/x', code: 'invalid' },
    ]);
  }],
  ['opaque photo', 'error', (store, accepted) => {
    const command = photoUploadCommand(accepted);
    store.setOpaquePhotoOutcome(accepted.metadata.id, {
      kind: 'photo-cutoff',
      command,
      attempt: attemptFor(command),
      observed: 'unavailable',
    });
  }],
];

describe('resume store state and replay', () => {
  it('keeps accepted separate and derives current by queue replay', () => {
    const { accepted, store } = initialized();
    const command = titleCommand(accepted, 'Optimistic');

    store.enqueue(accepted.metadata.id, command);
    const record = store.recordFor(accepted.metadata.id)!;

    expect(record.accepted.metadata.title).toBe(accepted.metadata.title);
    expect(record.current.metadata.title).toBe('Optimistic');
    expect(record.pending).toEqual([command]);
    expect(record.accepted).not.toBe(accepted);
  });

  it(
    'coalesces adjacent atomic commands but retains first dependencies',
    () => {
      const { accepted, store } = initialized();
      const first = titleCommand(accepted, 'First', 3, 'first');
      const second = {
        ...titleCommand(accepted, 'Second', 4, 'second'),
        dependencyIds: ['dependency-1'],
      } as AtomicEditorCommand;

      store.enqueue(accepted.metadata.id, first);
      store.enqueue(accepted.metadata.id, second);
      const record = store.recordFor(accepted.metadata.id)!;

      expect(record.pending).toHaveLength(1);
      expect(record.pending[0]).toMatchObject({
        id: 'first',
        sequence: 3,
        dependencyIds: [],
        kind: 'metadataField',
        value: 'Second',
      });
      expect(record.current.metadata.title).toBe('Second');
    },
  );

  it('derives sequence and dependencies from retained queue work', () => {
    const { accepted, store } = initialized();
    const first = titleCommand(accepted, 'First', 3, 'first');
    const second = titleCommand(accepted, 'Second', 5, 'second');

    store.enqueue(accepted.metadata.id, first);
    store.enqueue(accepted.metadata.id, second);
    const record = store.recordFor(accepted.metadata.id)!;

    expect(nextSequence(record)).toBe(4);
    expect(dependencyIdsForNewCommand(record)).toEqual(['first']);
  });

  it('replays a template group from its captured final snapshot', () => {
    const { accepted, store } = initialized();
    const intendedFinal = {
      document: accepted.document,
      metadata: { ...accepted.metadata, title: 'Template title' },
    };
    const group = {
      kind: 'templateGroup',
      id: 'template-1',
      sequence: 2,
      intendedFinal,
    } as EditorQueueItem;

    store.enqueue(accepted.metadata.id, group);

    expect(store.recordFor(accepted.metadata.id)!.accepted.metadata.title).toBe(
      accepted.metadata.title,
    );
    expect(store.recordFor(accepted.metadata.id)!.current.metadata.title).toBe(
      'Template title',
    );
  });

  it(
    'indexes issues and never reports queued work as saved',
    () => {
      const { accepted, store } = initialized();
      const command = titleCommand(accepted, 'Unsaved');

      expect(store.saveStateFor(accepted.metadata.id)).toBe('saved');
      store.enqueue(accepted.metadata.id, command);
      expect(store.saveStateFor(accepted.metadata.id)).toBe('dirty');
      store.startAttempt(
        accepted.metadata.id,
        command,
        command,
        attemptFor(command),
      );
      expect(store.saveStateFor(accepted.metadata.id)).toBe('saving');
      store.holdAttempt(accepted.metadata.id, {
        kind: 'failed',
        queueItem: command,
        command,
        attempt: attemptFor(command),
        reason: 'bad_request',
      });
      store.setIssues(accepted.metadata.id, command.id, [
        { path: '/metadata/title', code: 'required' },
      ]);

      expect(store.recordFor(accepted.metadata.id)!.issues).toEqual({
        [command.id]: [{ path: '/metadata/title', code: 'required' }],
      });
      expect(store.saveStateFor(accepted.metadata.id)).toBe('error');
    },
  );

  it.each(saveStateBlockers)('reports %s as %s', (_name, expected, arrange) => {
    const { accepted, store } = initialized();
    arrange(store, accepted);

    expect(store.saveStateFor(accepted.metadata.id)).toBe(expected);
  });

  it('keeps independent records and session state', () => {
    const { accepted, store } = initialized();
    const other = acceptedFixture({
      metadata: { ...accepted.metadata, id: 'resume-2' },
    });
    store.initialize(other);
    store.enqueue(accepted.metadata.id, titleCommand(accepted, 'First'));
    store.markSessionLost(accepted.metadata.id);
    store.setTemplateState(accepted.metadata.id, {
      kind: 'queued',
      nextChild: 0,
    });

    expect(store.recordFor(accepted.metadata.id)).toMatchObject({
      sessionLost: true,
      templateState: { kind: 'queued' },
    });
    expect(store.recordFor(other.metadata.id)).toMatchObject({
      pending: [],
      sessionLost: false,
    });
    expect(store.saveStateFor(accepted.metadata.id)).toBe('session-lost');
  });
});

describe('resume store adoption and teardown', () => {
  it('keeps an older successful command queued', () => {
    const { accepted, store } = initialized(
      acceptedFixture({ revision: parseRevision('5') }),
    );
    const command = titleCommand(accepted, 'Saved earlier');
    store.enqueue(accepted.metadata.id, command);
    const before = structuredClone(
      toRaw(store.recordFor(accepted.metadata.id)!),
    );

    expect(
      store.adoptComplete(
        accepted.metadata.id,
        acceptedFixture({ revision: parseRevision('4') }),
      ),
    ).toEqual({ kind: 'older', winner: before!.accepted });
    expect(store.recordFor(accepted.metadata.id)).toEqual(before);
  });

  it('leaves newer complete queue removal explicit', () => {
    const { accepted, store } = initialized(
      acceptedFixture({ revision: parseRevision('5') }),
    );
    const command = titleCommand(accepted, 'Saved');
    store.enqueue(accepted.metadata.id, command);
    const newer = acceptedFixture({
      revision: parseRevision('6'),
      metadata: { ...accepted.metadata, title: 'Server title' },
    });

    expect(store.adoptComplete(accepted.metadata.id, newer).kind).toBe(
      'adopted',
    );
    expect(store.recordFor(accepted.metadata.id)!.pending).toContainEqual(
      command,
    );
    store.dropHead(accepted.metadata.id, command.id);
    expect(store.recordFor(accepted.metadata.id)!.pending).toEqual([]);
  });

  it('keeps stale-winner summary metadata from the complete response', () => {
    const { accepted, store } = initialized(
      acceptedFixture({ revision: parseRevision('5') }),
    );
    const stale = acceptedFixture({
      revision: parseRevision('6'),
      metadata: { ...accepted.metadata, title: 'Untrusted summary' },
    });

    store.adoptStaleWinner(accepted.metadata.id, stale);
    expect(store.recordFor(accepted.metadata.id)!.accepted).toMatchObject({
      revision: parseRevision('6'),
      metadata: { title: accepted.metadata.title },
      metadataFreshness: 'stale',
    });
    store.adoptStaleWinner(
      accepted.metadata.id,
      acceptedFixture({ revision: parseRevision('5') }),
    );
    expect(store.recordFor(accepted.metadata.id)!.accepted.revision).toBe(
      parseRevision('6'),
    );
  });

  it('acknowledges only the current child and a newer parent tag', () => {
    const { accepted, store } = initialized(withEntry(acceptedFixture({
      revision: parseRevision('5'),
    })));
    const command = entryDeleteCommand(accepted);
    const retained = titleCommand(accepted, 'Retained', 2, 'retained-1');
    store.enqueue(accepted.metadata.id, command);
    store.enqueue(accepted.metadata.id, retained);
    store.startAttempt(
      accepted.metadata.id,
      command,
      command,
      attemptFor(command),
    );

    store.acknowledgeChild(
      accepted.metadata.id,
      'wrong-head',
      parentETag(parseRevision('6')),
    );
    expect(store.recordFor(accepted.metadata.id)!.accepted.revision).toBe(
      parseRevision('5'),
    );
    store.acknowledgeChild(
      accepted.metadata.id,
      command.id,
      parentETag(parseRevision('5')),
    );
    expect(store.recordFor(accepted.metadata.id)!.accepted.revision).toBe(
      parseRevision('5'),
    );
    store.acknowledgeChild(
      accepted.metadata.id,
      command.id,
      parentETag(parseRevision('6')),
    );
    expect(store.recordFor(accepted.metadata.id)!).toMatchObject({
      accepted: { revision: parseRevision('6'), metadataFreshness: 'stale' },
      completeReadRequired: true,
    });
    const record = store.recordFor(accepted.metadata.id)!;
    expect(record.accepted.document.content.profile!.entries).toEqual([]);
    expect(record.current.document.content.profile!.entries).toEqual([]);
    expect(record.current.metadata.title).toBe('Retained');
    expect(record.pending).toEqual([retained]);
    store.dropHead(accepted.metadata.id, command.id);
    store.dropHead(accepted.metadata.id, retained.id);
    expect(store.saveStateFor(accepted.metadata.id)).toBe('dirty');
  });

  it('adopts a validated same-revision complete read after a child ack', () => {
    const { accepted, store } = initialized(withEntry(acceptedFixture({
      revision: parseRevision('5'),
    })));
    const command = entryDeleteCommand(accepted);
    const retained = titleCommand(accepted, 'Retained', 2, 'retained-1');
    store.enqueue(accepted.metadata.id, command);
    store.enqueue(accepted.metadata.id, retained);
    store.startAttempt(
      accepted.metadata.id,
      command,
      command,
      attemptFor(command),
    );
    store.acknowledgeChild(
      accepted.metadata.id,
      command.id,
      parentETag(parseRevision('6')),
    );
    store.dropHead(accepted.metadata.id, command.id);

    const afterAck = store.recordFor(accepted.metadata.id)!;
    const complete = {
      ...structuredClone(toRaw(afterAck.accepted)),
      metadata: {
        ...structuredClone(toRaw(afterAck.accepted.metadata)),
        title: 'Server title',
      },
      metadataFreshness: 'fresh' as const,
    };
    expect(
      store.adoptCompleteRead(accepted.metadata.id, complete),
    ).toMatchObject({ kind: 'adopted' });
    expect(store.recordFor(accepted.metadata.id)!).toMatchObject({
      accepted: {
        revision: parseRevision('6'),
        metadata: { title: 'Server title' },
        metadataFreshness: 'fresh',
      },
      completeReadRequired: false,
      pending: [retained],
      current: { metadata: { title: 'Retained' } },
    });

    const older = acceptedFixture({ revision: parseRevision('5') });
    expect(
      store.adoptCompleteRead(accepted.metadata.id, older),
    ).toMatchObject({ kind: 'older' });
    expect(store.recordFor(accepted.metadata.id)!.accepted.revision).toBe(
      parseRevision('6'),
    );
  });

  it('acknowledges photo delete without retaining ready data', () => {
    const { accepted, store } = initialized(withPhoto(acceptedFixture({
      revision: parseRevision('5'),
    })));
    const command = photoDeleteCommand(accepted);
    store.setPhotoRead(accepted.metadata.id, readyPhoto());
    store.enqueue(accepted.metadata.id, command);
    store.startAttempt(
      accepted.metadata.id,
      command,
      command,
      attemptFor(command),
    );
    store.acknowledgeChild(
      accepted.metadata.id,
      command.id,
      parentETag(parseRevision('6')),
    );

    const record = store.recordFor(accepted.metadata.id)!;
    expect(record.accepted.document.personalDetails.photo).toBeUndefined();
    expect(record.current.document.personalDetails.photo).toBeUndefined();
    expect(record.photoRead).toEqual({ kind: 'none' });
    expect(record.accepted.revision).toBe(parseRevision('6'));
    expect(record.accepted.metadataFreshness).toBe('stale');
    expect(record.completeReadRequired).toBe(true);
  });

  it('removes a resume after definitive delete acknowledgement', () => {
    const { accepted, store } = initialized();
    const command = resumeDeleteCommand(accepted);
    store.enqueue(accepted.metadata.id, command);
    store.startAttempt(
      accepted.metadata.id,
      command,
      command,
      attemptFor(command),
    );

    store.acknowledgeResumeDelete(accepted.metadata.id, 'wrong-head');
    expect(store.recordFor(accepted.metadata.id)).toBeDefined();
    store.acknowledgeResumeDelete(accepted.metadata.id, command.id);
    expect(store.recordFor(accepted.metadata.id)).toBeUndefined();
  });

  it('rejects stale photo generations and mismatched bindings', () => {
    const accepted = acceptedFixture({
      document: {
        ...acceptedFixture().document,
        personalDetails: { photo: { key: 'photo-1' } },
      },
    });
    const { store } = initialized(accepted);
    const ready = {
      kind: 'ready' as const,
      binding: 'photo-1',
      generation: 2,
      etag: '"photo-1"' as never,
      dataUrl: 'data:image/png;base64,ready',
    };
    store.setPhotoRead(accepted.metadata.id, ready);
    store.setPhotoRead(accepted.metadata.id, {
      kind: 'loading',
      binding: 'photo-1',
      generation: 1,
    });
    expect(store.recordFor(accepted.metadata.id)!.photoRead).toEqual(ready);
    store.setPhotoRead(accepted.metadata.id, {
      kind: 'ready',
      binding: 'other-photo',
      generation: 3,
      etag: '"photo-2"' as never,
      dataUrl: 'data:image/png;base64,other',
    });
    expect(store.recordFor(accepted.metadata.id)!.photoRead).toMatchObject({
      kind: 'suspended',
      binding: 'other-photo',
      reason: 'binding-mismatch',
    });
  });

  it('clears ready bytes after complete and stale-winner key changes', () => {
    const { accepted, store } = initialized(withPhoto(acceptedFixture({
      revision: parseRevision('5'),
    })));
    store.setPhotoRead(accepted.metadata.id, readyPhoto());
    store.adoptComplete(
      accepted.metadata.id,
      withPhoto(acceptedFixture({ revision: parseRevision('6') }), 'photo-2'),
    );
    expect(store.recordFor(accepted.metadata.id)!.photoRead).toEqual({
      kind: 'none',
    });

    store.setPhotoRead(accepted.metadata.id, readyPhoto('photo-2'));
    store.adoptStaleWinner(
      accepted.metadata.id,
      withPhoto(acceptedFixture({ revision: parseRevision('7') }), 'photo-3'),
    );
    expect(store.recordFor(accepted.metadata.id)!.photoRead).toEqual({
      kind: 'none',
    });
  });

  it('clears ready bytes for complete photo removal and local discard', () => {
    const { accepted, store } = initialized(withPhoto(acceptedFixture({
      revision: parseRevision('5'),
    })));
    store.setPhotoRead(accepted.metadata.id, readyPhoto());
    store.adoptComplete(accepted.metadata.id, acceptedFixture({
      revision: parseRevision('6'),
    }));
    expect(store.recordFor(accepted.metadata.id)!.photoRead).toEqual({
      kind: 'none',
    });

    const withReadyPhoto = withPhoto(acceptedFixture());
    store.initialize(withReadyPhoto);
    store.setPhotoRead(withReadyPhoto.metadata.id, readyPhoto());
    store.enqueue(
      withReadyPhoto.metadata.id,
      photoDeleteCommand(withReadyPhoto),
    );
    store.discardLocal(withReadyPhoto.metadata.id);
    expect(store.recordFor(withReadyPhoto.metadata.id)!.photoRead).toEqual({
      kind: 'none',
    });
  });

  it('releases ready bytes on session loss and record removal', () => {
    const { accepted, store } = initialized(withPhoto(acceptedFixture()));
    store.setPhotoRead(accepted.metadata.id, readyPhoto());
    store.enqueue(accepted.metadata.id, titleCommand(accepted, 'Retained'));
    store.markSessionLost(accepted.metadata.id);

    expect(store.recordFor(accepted.metadata.id)).toMatchObject({
      sessionLost: true,
      pending: [expect.objectContaining({ id: 'command-1' })],
      photoRead: { kind: 'suspended', reason: 'session-lost' },
    });
    expect(store.recordFor(accepted.metadata.id)!.photoRead).not.toHaveProperty(
      'dataUrl',
    );
    store.removeResume(accepted.metadata.id);
    expect(store.recordFor(accepted.metadata.id)).toBeUndefined();
  });

  it('clears only session loss while retaining work', () => {
    const { accepted, store } = initialized(withPhoto(acceptedFixture()));
    const command = titleCommand(accepted, 'In flight');
    const retained = titleCommand(accepted, 'Queued', 2, 'queued-1');
    store.enqueue(accepted.metadata.id, command);
    store.startAttempt(
      accepted.metadata.id,
      command,
      command,
      attemptFor(command),
    );
    store.enqueue(accepted.metadata.id, retained);
    store.setIssues(accepted.metadata.id, command.id, [
      { path: '/metadata/title', code: 'invalid' },
    ]);
    store.markConflict(accepted.metadata.id, { id: 'conflict-1' } as never);
    store.setTemplateState(accepted.metadata.id, { kind: 'partial' } as never);
    const opaqueCommand = photoUploadCommand(accepted);
    store.setOpaquePhotoOutcome(accepted.metadata.id, {
      kind: 'photo-cutoff',
      command: opaqueCommand,
      attempt: attemptFor(opaqueCommand),
      observed: 'unavailable',
    });
    store.setPhotoRead(accepted.metadata.id, readyPhoto());
    store.markSessionLost(accepted.metadata.id);
    const before = structuredClone(
      toRaw(store.recordFor(accepted.metadata.id)!),
    );
    expect(before.attempt).not.toBeNull();
    expect(before.pending).toEqual([retained]);
    expect(before.conflicts).toHaveLength(1);
    expect(before.photoRead).toMatchObject({
      kind: 'suspended',
      reason: 'session-lost',
    });

    store.clearSessionLost(accepted.metadata.id);

    const record = store.recordFor(accepted.metadata.id)!;
    expect(record.sessionLost).toBe(false);
    expect(record.accepted).toEqual(before.accepted);
    expect(record.current).toEqual(before.current);
    expect(record.pending).toEqual(before.pending);
    expect(record.attempt).toEqual(before.attempt);
    expect(record.conflicts).toEqual(before.conflicts);
    expect(record.issues).toEqual(before.issues);
    expect(record.templateState).toEqual(before.templateState);
    expect(record.opaquePhotoOutcome).toEqual(before.opaquePhotoOutcome);
    expect(record.photoRead).toEqual(before.photoRead);
    store.clearSessionLost('missing-resume');
    expect(store.recordFor(accepted.metadata.id)).toEqual(record);
  });

  it('resolves only the exact conflict and retains all other state', () => {
    const { accepted, store } = initialized(withPhoto(acceptedFixture()));
    const command = titleCommand(accepted, 'In flight');
    const retained = titleCommand(accepted, 'Queued', 2, 'queued-1');
    store.enqueue(accepted.metadata.id, command);
    store.startAttempt(
      accepted.metadata.id,
      command,
      command,
      attemptFor(command),
    );
    store.enqueue(accepted.metadata.id, retained);
    store.setIssues(accepted.metadata.id, command.id, [
      { path: '/metadata/title', code: 'invalid' },
    ]);
    store.setTemplateState(accepted.metadata.id, { kind: 'partial' } as never);
    const opaqueCommand = photoUploadCommand(accepted);
    store.setOpaquePhotoOutcome(accepted.metadata.id, {
      kind: 'photo-cutoff',
      command: opaqueCommand,
      attempt: attemptFor(opaqueCommand),
      observed: 'unavailable',
    });
    store.setPhotoRead(accepted.metadata.id, readyPhoto());
    store.markSessionLost(accepted.metadata.id);
    store.markConflict(accepted.metadata.id, { id: 'resolve-me' } as never);
    store.markConflict(accepted.metadata.id, { id: 'keep-me' } as never);
    const before = store.recordFor(accepted.metadata.id)!;
    const retainedState = {
      accepted: before.accepted,
      current: before.current,
      pending: before.pending,
      attempt: before.attempt,
      issues: before.issues,
      templateState: before.templateState,
      sessionLost: before.sessionLost,
      photoRead: before.photoRead,
      opaquePhotoOutcome: before.opaquePhotoOutcome,
    };

    store.resolveConflict(accepted.metadata.id, 'resolve-me');

    const record = store.recordFor(accepted.metadata.id)!;
    expect(record.conflicts).toEqual([{ id: 'keep-me' }]);
    expect(record.accepted).toEqual(retainedState.accepted);
    expect(record.current).toEqual(retainedState.current);
    expect(record.pending).toEqual(retainedState.pending);
    expect(record.attempt).toEqual(retainedState.attempt);
    expect(record.issues).toEqual(retainedState.issues);
    expect(record.templateState).toEqual(retainedState.templateState);
    expect(record.sessionLost).toBe(retainedState.sessionLost);
    expect(record.photoRead).toEqual(retainedState.photoRead);
    expect(record.opaquePhotoOutcome).toEqual(retainedState.opaquePhotoOutcome);
    store.resolveConflict(accepted.metadata.id, 'missing-conflict');
    store.resolveConflict('missing-resume', 'resolve-me');
    expect(store.recordFor(accepted.metadata.id)).toEqual(record);
  });

  it('continues only the active template group ahead of later work', () => {
    const { accepted, store } = initialized(withPhoto(acceptedFixture()));
    const group = {
      kind: 'templateGroup',
      id: 'template-1',
      sequence: 1,
      intendedFinal: {
        document: accepted.document,
        metadata: { ...accepted.metadata, title: 'Template' },
      },
    } as EditorQueueItem;
    const activeCommand = titleCommand(accepted, 'Template child');
    const later = titleCommand(accepted, 'Later', 2, 'later-1');
    store.enqueue(accepted.metadata.id, group);
    store.enqueue(accepted.metadata.id, later);
    store.startAttempt(
      accepted.metadata.id,
      group,
      activeCommand,
      attemptFor(activeCommand),
    );
    store.setIssues(accepted.metadata.id, activeCommand.id, [
      { path: '/metadata/title', code: 'invalid' },
    ]);
    store.setTemplateState(accepted.metadata.id, { kind: 'partial' } as never);
    store.markConflict(accepted.metadata.id, { id: 'conflict-1' } as never);
    store.setPhotoRead(accepted.metadata.id, readyPhoto());
    store.markSessionLost(accepted.metadata.id);
    const before = store.recordFor(accepted.metadata.id)!;
    const retainedState = {
      accepted: before.accepted,
      issues: before.issues,
      templateState: before.templateState,
      conflicts: before.conflicts,
      photoRead: before.photoRead,
      sessionLost: before.sessionLost,
      opaquePhotoOutcome: before.opaquePhotoOutcome,
    };

    store.continueTemplateGroup(accepted.metadata.id, 'wrong-group');
    expect(store.recordFor(accepted.metadata.id)!.attempt).toEqual(
      before.attempt,
    );
    expect(store.recordFor(accepted.metadata.id)!.pending).toEqual([later]);

    store.continueTemplateGroup(accepted.metadata.id, group.id);

    const record = store.recordFor(accepted.metadata.id)!;
    expect(record.attempt).toBeNull();
    expect(record.pending).toEqual([group, later]);
    expect(record.current.metadata.title).toBe('Later');
    expect(record.accepted).toEqual(retainedState.accepted);
    expect(record.issues).toEqual(retainedState.issues);
    expect(record.templateState).toEqual(retainedState.templateState);
    expect(record.conflicts).toEqual(retainedState.conflicts);
    expect(record.photoRead).toEqual(retainedState.photoRead);
    expect(record.sessionLost).toBe(retainedState.sessionLost);
    expect(record.opaquePhotoOutcome).toEqual(retainedState.opaquePhotoOutcome);
    store.continueTemplateGroup(accepted.metadata.id, group.id);
    store.continueTemplateGroup('missing-resume', group.id);
    expect(store.recordFor(accepted.metadata.id)).toEqual(record);
  });

  it('does not continue an atomic active attempt', () => {
    const { accepted, store } = initialized();
    const command = titleCommand(accepted, 'Atomic');
    store.enqueue(accepted.metadata.id, command);
    store.startAttempt(
      accepted.metadata.id,
      command,
      command,
      attemptFor(command),
    );
    const before = store.recordFor(accepted.metadata.id)!;

    store.continueTemplateGroup(accepted.metadata.id, 'template-1');

    expect(store.recordFor(accepted.metadata.id)).toEqual(before);
  });
});
