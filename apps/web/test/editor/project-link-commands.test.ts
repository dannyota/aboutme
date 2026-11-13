import type { Section } from '@aboutme/schema';
import { CURRENT_VERSION } from '@aboutme/schema/released';
import { createPinia, setActivePinia } from 'pinia';
import { computed, effectScope } from 'vue';
import { afterEach, describe, expect, it, vi } from 'vitest';

import type {
  ProjectLibrary,
} from '../../app/components/editor/forms/projectLibrary';
import {
  useProjectLinking,
} from '../../app/components/editor/forms/projectLinking';
import {
  createResumeEditorActions,
} from '../../app/composables/useResumeEditor';
import { createMutationCoordinator } from '../../app/editor/coordinator';
import {
  captureCommand,
  replayCommand,
  type AtomicCommandIntent,
} from '../../app/editor/commands';
import { takeProjectOutcome } from '../../app/editor/projectOutcomes';
import { reconcileCommand } from '../../app/editor/reconcile';
import {
  createResumeApi,
  freezeAttempt,
  requestFromAttempt,
} from '../../app/editor/resumeApi';
import { parseRevision } from '../../app/editor/revision';
import {
  captureTemplateGroup,
  templateUndoAvailable,
} from '../../app/editor/templateGroup';
import { TEMPLATES } from '@aboutme/schema/templates';
import type { AcceptedResume, EditorRuntime } from '../../app/editor/types';
import {
  dependencyIdsForNewCommand,
  nextSequence,
  useResumeStore,
} from '../../app/stores/resumes';
import { acceptedFixture } from './fixture';

const PROJECT = '0190f000-0000-7000-8000-0000000000c1';
const ENTRY = '0190f000-0000-7000-8000-0000000000e1';

type Entry = Section['entries'][number];

function withEntries(
  entries: readonly Entry[],
  revision = '1',
): AcceptedResume {
  const base = acceptedFixture();
  return acceptedFixture({
    document: {
      ...base.document,
      content: {
        projects: { sectionType: 'project', entries: [...entries] } as Section,
      },
      customization: {
        ...base.document.customization,
        layout: {
          ...base.document.customization.layout,
          sections: { main: ['projects'], sidebar: [] },
        },
      },
    },
    revision: parseRevision(revision),
  });
}

const linked = (overrides: Partial<Entry> = {}): Entry => ({
  id: ENTRY,
  title: 'Shop site',
  projectId: PROJECT,
  ...overrides,
} as Entry);

function runtime(ids: string[]): EditorRuntime {
  return {
    nowEpochMs: () => 0,
    uuid: () => ids.shift() ?? 'spare',
    delay: async () => {},
  };
}

function capture(accepted: AcceptedResume, intent: AtomicCommandIntent) {
  return captureCommand(accepted, {
    resumeId: accepted.metadata.id,
    ownerId: 'owner-1',
    sequence: 1,
    dependencyIds: [],
    intent,
  }, runtime(['command-1']));
}

const auth = {
  user: computed(() => ({ id: 'owner-1' })),
  csrfToken: computed(() => 'csrf-1'),
  authState: computed(() => 'authenticated'),
} as never;

function queued(accepted: AcceptedResume, intent: AtomicCommandIntent) {
  setActivePinia(createPinia());
  const store = useResumeStore();
  store.initialize(accepted);
  store.enqueue(accepted.metadata.id, capture(accepted, intent));
  const api = {
    dispatch: vi.fn(),
    read: vi.fn(),
  };
  const coordinator = createMutationCoordinator({
    api: api as never,
    store,
    auth,
    runtime: runtime(['attempt-1', 'attempt-2', 'attempt-3']),
  });
  const id = accepted.metadata.id;
  return { store, api, coordinator, id };
}

const OTHER = '0190f000-0000-7000-8000-0000000000e9';

/** Queues each intent on the replayed state, as the editor's actions do. */
function queuedAll(
  accepted: AcceptedResume,
  intents: readonly AtomicCommandIntent[],
) {
  setActivePinia(createPinia());
  const store = useResumeStore();
  const id = accepted.metadata.id;
  store.initialize(accepted);
  intents.forEach((intent, index) => {
    const record = store.recordFor(id)!;
    store.enqueue(id, captureCommand(record.current, {
      resumeId: id,
      ownerId: 'owner-1',
      sequence: nextSequence(record),
      dependencyIds: dependencyIdsForNewCommand(record),
      intent,
    }, runtime([`command-${index + 1}`])));
  });
  const api = { dispatch: vi.fn(), read: vi.fn() };
  const coordinator = createMutationCoordinator({
    api: api as never,
    store,
    auth,
    runtime: runtime(['attempt-1', 'attempt-2', 'attempt-3', 'attempt-4']),
  });
  return { store, api, coordinator, id };
}

const typed = (value: string): AtomicCommandIntent => ({
  kind: 'entryField',
  sectionKey: 'projects',
  entryId: ENTRY,
  path: 'subtitle',
  value: { present: true, value },
});

function stale(winner: AcceptedResume) {
  return {
    kind: 'stale', status: 412,
    winner: { document: winner.document, revision: winner.revision },
  };
}

function complete(accepted: AcceptedResume) {
  return { kind: 'complete', status: 200, accepted };
}

afterEach(() => {
  vi.useRealTimers();
});

describe('project link command', () => {
  it('captures unlinked as its base and linked as its intent, and changes '
    + 'nothing locally', () => {
    const accepted = withEntries([]);
    const command = capture(accepted, {
      kind: 'projectLink', projectId: PROJECT,
    });

    expect(command.base.target).toEqual({ present: true, value: false });
    expect(command.intended?.target).toEqual({ present: true, value: true });
    expect(replayCommand(accepted, command).document)
      .toEqual(accepted.document);
  });

  it('is sent as a POST of the project id with the resume revision', () => {
    const accepted = withEntries([], '7');
    const command = capture(accepted, {
      kind: 'projectLink', projectId: PROJECT,
    });

    const request = requestFromAttempt(
      freezeAttempt(command, accepted, runtime(['key-1'])),
      'csrf-1',
    );

    expect(request.method).toBe('POST');
    expect(new URL(request.url, 'http://local').pathname)
      .toBe('/api/v1/resumes/resume-1/project-links');
    expect(request.headers.get('If-Match')).toBe('"r7"');
    expect(request.headers.get('Idempotency-Key')).toBe('key-1');
    expect(request.headers.get('X-Resume-Schema-Version'))
      .toBe(String(CURRENT_VERSION));
    expect(request.headers.get('Content-Type')).toBe('application/json');
  });

  it('is satisfied without a request when the resume already links it',
    async () => {
      const accepted = withEntries([linked()]);
      const command = capture(withEntries([]), {
        kind: 'projectLink', projectId: PROJECT,
      });
      expect(reconcileCommand(command, accepted).kind).toBe('satisfied');

      const { store, api, coordinator, id } = queued(withEntries([]), {
        kind: 'projectLink', projectId: PROJECT,
      });
      store.adoptComplete(id, withEntries([linked()], '2'));
      await coordinator.flush(id);

      expect(api.dispatch).not.toHaveBeenCalled();
      expect(store.recordFor(id)?.pending).toEqual([]);
    });

  it('adopts the entry-write response and leaves the queue empty',
    async () => {
      const { store, api, coordinator, id } = queued(withEntries([]), {
        kind: 'projectLink', projectId: PROJECT,
      });
      api.dispatch.mockResolvedValueOnce({
        kind: 'complete', status: 200, accepted: withEntries([linked()], '2'),
      });

      await coordinator.flush(id);

      const record = store.recordFor(id)!;
      expect(record.pending).toEqual([]);
      expect(record.attempt).toBeNull();
      expect(record.current.document.content.projects?.entries)
        .toEqual([linked()]);
      expect(store.saveStateFor(id)).toBe('saved');
    });

  it('resends the same link once on a revision mismatch, deciding nothing '
    + 'again', async () => {
    const { api, coordinator, id, store } = queued(withEntries([]), {
      kind: 'projectLink', projectId: PROJECT,
    });
    api.dispatch
      .mockResolvedValueOnce(stale(withEntries([], '4')))
      .mockResolvedValueOnce(complete(withEntries([linked()], '5')));

    await coordinator.flush(id);

    const attempts = api.dispatch.mock.calls.map(([attempt]) => attempt);
    expect(attempts.map((attempt) => [attempt.operation, attempt.ifMatch]))
      .toEqual([
        ['linkResumeProject', '"r1"'],
        ['linkResumeProject', '"r4"'],
      ]);
    expect(attempts[1].payload).toEqual(attempts[0].payload);
    expect(attempts[1].idempotencyKey).not.toBe(attempts[0].idempotencyKey);
    expect(api.read).not.toHaveBeenCalled();
    expect(takeProjectOutcome('command-1')).toBeUndefined();
    const record = store.recordFor(id)!;
    expect(record.attempt).toBeNull();
    expect(record.completeReadRequired).toBe(false);
  });

  it.each([
    ['a missing project', { kind: 'rejected', code: 'project_not_found' }],
    ['an existing link', { kind: 'rejected', code: 'project_already_linked' }],
    ['an invalid document', {
      kind: 'validation-rejected',
      issues: [{ path: 'content.projects.entries[0].title', code: 'too_long' }],
    }],
  ])('drops a link refused for %s, rereads and keeps saving', async (
    _name, refusal,
  ) => {
    const { store, api, coordinator, id } = queued(withEntries([]), {
      kind: 'projectLink', projectId: PROJECT,
    });
    api.dispatch.mockResolvedValueOnce(refusal);
    api.read.mockResolvedValue({
      kind: 'complete', accepted: withEntries([], '1'),
    });

    await coordinator.flush(id);

    const record = store.recordFor(id)!;
    expect(record.attempt).toBeNull();
    expect(record.pending).toEqual([]);
    expect(record.issues).toEqual({});
    expect(api.read).toHaveBeenCalledOnce();
    expect(store.saveStateFor(id)).toBe('saved');
  });

  it('is sent without the typing debounce', async () => {
    vi.useFakeTimers();
    const { api, coordinator, id } = queued(withEntries([]), {
      kind: 'projectLink', projectId: PROJECT,
    });
    api.dispatch.mockResolvedValue({
      kind: 'complete', status: 200, accepted: withEntries([linked()], '2'),
    });

    coordinator.schedule(id);
    await vi.advanceTimersByTimeAsync(0);

    expect(api.dispatch).toHaveBeenCalledOnce();
  });
});

describe('project refresh command', () => {
  it('captures the link and its text as its base, has no predicted result '
    + 'and changes nothing locally', () => {
    const accepted = withEntries([linked({
      subtitle: 'Lead', link: 'https://shop.example', description: '<p>A</p>',
      dates: { start: { y: 2024 }, end: null, present: true }, isHidden: true,
    })]);
    const command = capture(accepted, {
      kind: 'projectRefresh', sectionKey: 'projects', entryId: ENTRY,
    });

    expect(command.base.target).toEqual({
      present: true,
      value: {
        projectId: PROJECT, title: 'Shop site', subtitle: 'Lead',
        link: 'https://shop.example', description: '<p>A</p>',
        dates: { start: { y: 2024 }, end: null, present: true },
      },
    });
  });

  it('captures the link as its base, has no predicted result and changes '
    + 'nothing locally', () => {
    const accepted = withEntries([linked()]);
    const command = capture(accepted, {
      kind: 'projectRefresh', sectionKey: 'projects', entryId: ENTRY,
    });

    expect(command.base.target).toEqual({
      present: true, value: { projectId: PROJECT, title: 'Shop site' },
    });
    expect(command.intended).toBeNull();
    expect(replayCommand(accepted, command).document)
      .toEqual(accepted.document);
  });

  it('is sent as a bodiless POST on the entry', async () => {
    const accepted = withEntries([linked()], '3');
    const command = capture(accepted, {
      kind: 'projectRefresh', sectionKey: 'projects', entryId: ENTRY,
    });

    const request = requestFromAttempt(
      freezeAttempt(command, accepted, runtime(['key-1'])),
      'csrf-1',
    );

    expect(request.method).toBe('POST');
    expect(new URL(request.url, 'http://local').pathname)
      .toBe(`/api/v1/resumes/resume-1/entries/${ENTRY}/project-refresh`);
    expect(request.headers.get('If-Match')).toBe('"r3"');
    expect(request.headers.get('Content-Type')).toBeNull();
    expect(await request.text()).toBe('');
  });

  it('is never sent over text that changed after the person confirmed',
    () => {
      const command = capture(withEntries([linked()]), {
        kind: 'projectRefresh', sectionKey: 'projects', entryId: ENTRY,
      });

      expect(reconcileCommand(
        command, withEntries([linked({ isHidden: true })], '2'),
      ).kind).toBe('safe-base');
      for (const changed of [
        { title: 'Edited' }, { subtitle: 'New' }, { link: 'https://x.example' },
        { description: '<p>New</p>' },
        { dates: { start: { y: 2025 }, end: null, present: true } },
      ]) {
        expect(reconcileCommand(
          command, withEntries([linked(changed as Partial<Entry>)], '2'),
        ).kind).toBe('conflict');
      }
      expect(reconcileCommand(
        command, withEntries([{ id: ENTRY, title: 'Shop site' }], '2'),
      ).kind).toBe('conflict');
    });

  it('drops a refresh whose text changed elsewhere, keeps that text and '
    + 'says so, without resending', async () => {
    const { store, api, coordinator, id } = queued(withEntries([linked()]), {
      kind: 'projectRefresh', sectionKey: 'projects', entryId: ENTRY,
    });
    const winner = withEntries([linked({ title: 'Newer elsewhere' })], '2');
    api.dispatch.mockResolvedValueOnce(stale(winner));
    api.read.mockResolvedValue({ kind: 'complete', accepted: winner });

    await coordinator.flush(id);

    expect(api.dispatch).toHaveBeenCalledOnce();
    expect(takeProjectOutcome('command-1')).toBe('refresh-changed');
    const record = store.recordFor(id)!;
    expect(record.conflicts).toEqual([]);
    expect(record.attempt).toBeNull();
    expect(record.current.document.content.projects?.entries)
      .toEqual([linked({ title: 'Newer elsewhere' })]);
  });

  it('replaces the entry with the server text and keeps saving', async () => {
    const { store, api, coordinator, id } = queued(withEntries([linked({
      title: 'Old', subtitle: 'Old role',
    })]), { kind: 'projectRefresh', sectionKey: 'projects', entryId: ENTRY });
    api.dispatch.mockResolvedValueOnce({
      kind: 'complete', status: 200,
      accepted: withEntries([linked({ title: 'Shop site' })], '2'),
    });

    await coordinator.flush(id);

    expect(store.recordFor(id)?.current.document.content.projects?.entries)
      .toEqual([linked({ title: 'Shop site' })]);
    expect(store.saveStateFor(id)).toBe('saved');
  });

  it('drops a refresh of an entry whose project is gone and rereads',
    async () => {
      const { store, api, coordinator, id } = queued(withEntries([linked()]), {
        kind: 'projectRefresh', sectionKey: 'projects', entryId: ENTRY,
      });
      api.dispatch.mockResolvedValueOnce({
        kind: 'rejected', code: 'project_not_found',
      });
      api.read.mockResolvedValue({
        kind: 'complete',
        accepted: withEntries([{ id: ENTRY, title: 'Shop site' }], '2'),
      });

      await coordinator.flush(id);

      const record = store.recordFor(id)!;
      expect(record.attempt).toBeNull();
      expect(record.current.document.content.projects?.entries)
        .toEqual([{ id: ENTRY, title: 'Shop site' }]);
    });

  it('keeps template undo available as an entry edit does, while a link '
    + 'that adds a section ends it as adding a section does', () => {
    const accepted = withEntries([linked()]);
    const preset = TEMPLATES.find(({ customization }) =>
      customization.layout.placement === 'byType')!;
    const group = captureTemplateGroup({
      resumeId: accepted.metadata.id,
      ownerId: 'owner-1',
      sequence: 1,
      current: accepted,
      preset,
      dependencyIds: [],
      runtime: runtime(['group-1', 'child-1', 'child-2']),
    })!;
    const applied = { ...accepted, ...group.intendedFinal };
    const undo = {
      finalTarget: group.intended,
      contentContext: group.contentContext,
    };
    const refreshed = withEntries([linked({ title: 'Refreshed' })]);
    const linkedElsewhere = {
      ...applied,
      document: {
        ...applied.document,
        content: {
          ...applied.document.content,
          projects: refreshed.document.content.projects!,
        },
      },
    };

    const sectionCreated = {
      ...applied,
      document: {
        ...applied.document,
        content: {
          ...applied.document.content,
          linked: { sectionType: 'project', entries: [linked()] } as Section,
        },
        customization: {
          ...applied.document.customization,
          layout: {
            ...applied.document.customization.layout,
            sections: {
              ...applied.document.customization.layout.sections,
              main: [
                ...applied.document.customization.layout.sections.main,
                'linked',
              ],
            },
          },
        },
      },
    };

    expect(templateUndoAvailable(undo as never, applied)).toBe(true);
    expect(templateUndoAvailable(undo as never, linkedElsewhere)).toBe(true);
    expect(templateUndoAvailable(undo as never, sectionCreated)).toBe(false);
  });
});

describe('the edits queued behind a project command', () => {
  it('are sent after a 412 shows the link already landed', async () => {
    const { store, api, coordinator, id } = queuedAll(withEntries([
      { id: ENTRY, title: 'Typed' } as Entry,
    ]), [{ kind: 'projectLink', projectId: OTHER }, typed('Lead')]);
    const landed = withEntries([
      { id: ENTRY, title: 'Typed' } as Entry,
      { id: OTHER, projectId: OTHER } as Entry,
    ], '2');
    api.dispatch
      .mockResolvedValueOnce(stale(landed))
      .mockResolvedValueOnce(complete(withEntries([
        { id: ENTRY, title: 'Typed', subtitle: 'Lead' } as Entry,
        { id: OTHER, projectId: OTHER } as Entry,
      ], '3')));
    api.read.mockResolvedValue({ kind: 'complete', accepted: landed });

    await coordinator.flush(id);

    const attempts = api.dispatch.mock.calls.map(([attempt]) => attempt);
    expect(attempts.map((attempt) => [attempt.operation, attempt.ifMatch]))
      .toEqual([
        ['linkResumeProject', '"r1"'],
        ['upsertResumeEntry', '"r2"'],
      ]);
    expect(store.recordFor(id)?.pending).toEqual([]);
    expect(store.saveStateFor(id)).toBe('saved');
  });

  it('are sent after a rebased refresh is refused', async () => {
    const { store, api, coordinator, id } = queuedAll(
      withEntries([linked()]),
      [
        { kind: 'projectRefresh', sectionKey: 'projects', entryId: ENTRY },
        typed('Lead'),
      ],
    );
    const winner = withEntries([linked({ isHidden: true })], '2');
    api.dispatch
      .mockResolvedValueOnce(stale(winner))
      .mockResolvedValueOnce({ kind: 'rejected', code: 'project_not_found' })
      .mockResolvedValueOnce(complete(withEntries([
        { id: ENTRY, title: 'Shop site', subtitle: 'Lead', isHidden: true },
      ], '3')));
    api.read.mockResolvedValue({
      kind: 'complete',
      accepted: withEntries([
        { id: ENTRY, title: 'Shop site', isHidden: true } as Entry,
      ], '2'),
    });

    await coordinator.flush(id);

    expect(api.dispatch.mock.calls.map(([attempt]) =>
      [attempt.operation, attempt.ifMatch])).toEqual([
      ['refreshResumeProjectEntry', '"r1"'],
      ['refreshResumeProjectEntry', '"r2"'],
      ['upsertResumeEntry', '"r2"'],
    ]);
    expect(store.recordFor(id)?.pending).toEqual([]);
  });

  it('keep their order around a link, and the newest text stays', async () => {
    const { store, api, coordinator, id } = queuedAll(withEntries([
      { id: ENTRY, title: 'Typed' } as Entry,
    ]), [
      typed('Before'),
      { kind: 'projectLink', projectId: OTHER },
      typed('After'),
    ]);
    const entry = (subtitle: string) =>
      ({ id: ENTRY, title: 'Typed', subtitle }) as Entry;
    const other = { id: OTHER, projectId: OTHER } as Entry;
    api.dispatch
      .mockResolvedValueOnce(complete(withEntries([entry('Before')], '2')))
      .mockResolvedValueOnce(complete(withEntries(
        [entry('Before'), other], '3',
      )))
      .mockResolvedValueOnce(complete(withEntries(
        [entry('After'), other], '4',
      )));

    await coordinator.flush(id);

    expect(api.dispatch.mock.calls.map(([attempt]) => attempt.operation))
      .toEqual(['upsertResumeEntry', 'linkResumeProject', 'upsertResumeEntry']);
    expect(JSON.parse(api.dispatch.mock.calls[2]![0].payload.utf8).entry
      .subtitle).toBe('After');
    expect(store.recordFor(id)?.current.document.content.projects?.entries[0])
      .toEqual(entry('After'));
  });

  it('keep their order around a refresh, and text typed before the '
    + 'refresh landed is not sent over it unasked', async () => {
    const { store, api, coordinator, id } = queuedAll(withEntries([linked()]), [
      typed('Before'),
      { kind: 'projectRefresh', sectionKey: 'projects', entryId: ENTRY },
      typed('After'),
    ]);
    api.dispatch
      .mockResolvedValueOnce(complete(withEntries(
        [linked({ subtitle: 'Before' })], '2',
      )))
      .mockResolvedValueOnce(complete(withEntries(
        [linked({ subtitle: 'Library role' })], '3',
      )));

    await coordinator.flush(id);

    expect(api.dispatch.mock.calls.map(([attempt]) => attempt.operation))
      .toEqual(['upsertResumeEntry', 'refreshResumeProjectEntry']);
    const record = store.recordFor(id)!;
    expect(record.accepted.document.content.projects?.entries)
      .toEqual([linked({ subtitle: 'Library role' })]);
    expect(record.conflicts.map((conflict) => conflict.id))
      .toEqual(['command-3']);
    expect(record.current.document.content.projects?.entries)
      .toEqual([linked({ subtitle: 'After' })]);
  });
});

describe('a refresh meeting newer text on its retry', () => {
  it('is dropped with a notice when the second 412 brings changed text, '
    + 'and the edit behind it is sent', async () => {
    const { store, api, coordinator, id } = queuedAll(withEntries([
      linked(), { id: OTHER, title: 'Other' } as Entry,
    ]), [
      { kind: 'projectRefresh', sectionKey: 'projects', entryId: ENTRY },
      {
        kind: 'entryField', sectionKey: 'projects', entryId: OTHER,
        path: 'subtitle', value: { present: true, value: 'Lead' },
      },
    ]);
    const other = { id: OTHER, title: 'Other' } as Entry;
    const changed = withEntries([linked({ title: 'Newer' }), other], '3');
    api.dispatch
      .mockResolvedValueOnce(stale(withEntries(
        [linked({ isHidden: true }), other], '2',
      )))
      .mockResolvedValueOnce(stale(changed))
      .mockResolvedValueOnce(complete(withEntries([
        linked({ title: 'Newer' }), { ...other, subtitle: 'Lead' } as Entry,
      ], '4')));
    api.read.mockResolvedValue({ kind: 'complete', accepted: changed });

    await coordinator.flush(id);

    expect(api.dispatch.mock.calls.map(([attempt]) =>
      [attempt.operation, attempt.ifMatch])).toEqual([
      ['refreshResumeProjectEntry', '"r1"'],
      ['refreshResumeProjectEntry', '"r2"'],
      ['upsertResumeEntry', '"r3"'],
    ]);
    expect(takeProjectOutcome('command-1')).toBe('refresh-changed');
    const record = store.recordFor(id)!;
    expect(record.attempt).toBeNull();
    expect(record.pending).toEqual([]);
    expect(record.accepted.document.content.projects?.entries[0])
      .toEqual(linked({ title: 'Newer' }));
  });

  it('stops after three rebases when every 412 changed nothing it '
    + 'overwrites', async () => {
    const { store, api, coordinator, id } = queued(withEntries([linked()]), {
      kind: 'projectRefresh', sectionKey: 'projects', entryId: ENTRY,
    });
    for (const revision of ['2', '3', '4', '5']) {
      api.dispatch.mockResolvedValueOnce(stale(withEntries([
        linked({ isHidden: Number(revision) % 2 === 0 }),
      ], revision)));
    }

    await coordinator.flush(id);

    expect(api.dispatch).toHaveBeenCalledTimes(4);
    expect(store.recordFor(id)?.attempt).toMatchObject({
      kind: 'failed', reason: 'stale-limit',
    });
  });
});

describe('a refresh dropped after a realtime read', () => {
  it('takes its own conflict with it and leaves the others', async () => {
    const { store, api, coordinator, id } = queued(withEntries([linked()]), {
      kind: 'projectRefresh', sectionKey: 'projects', entryId: ENTRY,
    });
    const changed = withEntries([linked({ title: 'Newer' })], '2');
    store.markConflict(id, {
      id: 'unrelated', subject: 'atomic', kind: 'target-changed',
    } as never);
    api.readConditional = vi.fn().mockResolvedValue({
      kind: 'complete', accepted: changed, etag: '"r2"',
    }) as never;
    api.dispatch.mockImplementationOnce(async () => {
      await coordinator.refreshConditionalAndReconcile!(id);
      expect(store.recordFor(id)?.conflicts.map((item) => item.id))
        .toEqual(['unrelated', 'command-1']);
      return stale(changed);
    });
    api.read.mockResolvedValue({ kind: 'complete', accepted: changed });

    await coordinator.flush(id);

    const record = store.recordFor(id)!;
    expect(record.attempt).toBeNull();
    expect(record.conflicts.map((item) => item.id)).toEqual(['unrelated']);
  });
});

describe('a link prepared while edits go out', () => {
  it('sends a field edit whose debounce ends during the privacy reads, '
    + 'then drops the link a realtime read shows already landed', async () => {
    vi.useFakeTimers();
    setActivePinia(createPinia());
    const store = useResumeStore();
    const accepted = withEntries([{ id: ENTRY, title: 'Typed' } as Entry]);
    const id = accepted.metadata.id;
    store.initialize(accepted);
    const api = {
      dispatch: vi.fn(), read: vi.fn(), readConditional: vi.fn(),
    };
    const editorRuntime = runtime(['edit-1', 'attempt-1', 'link-1', 'spare']);
    const coordinator = createMutationCoordinator({
      api: api as never, store, auth, runtime: editorRuntime,
    });
    const actions = createResumeEditorActions({
      resumeId: id, store, coordinator, auth, runtime: editorRuntime,
    });
    let finishReads!: () => void;
    const reads = new Promise<void>((resolve) => {
      finishReads = resolve;
    });
    const onLinked = vi.fn();
    const linking = effectScope().run(() => useProjectLinking({
      actions,
      sectionKey: () => 'projects',
      library: {
        load: async () => {},
        find: () => undefined,
        published: async () => {
          await reads;
          return [{ id, live: false }];
        },
        linkedTo: async () => {
          await reads;
          return [];
        },
      } as unknown as ProjectLibrary,
      onLinked,
    }))!;
    const landed = withEntries([
      { id: ENTRY, title: 'Typed', subtitle: 'Lead' } as Entry,
      { id: OTHER, projectId: PROJECT } as Entry,
    ], '3');

    const choosing = linking.choose(PROJECT);
    actions.edit(typed('Lead'));
    api.dispatch.mockResolvedValueOnce(complete(withEntries([
      { id: ENTRY, title: 'Typed', subtitle: 'Lead' } as Entry,
    ], '2')));
    await vi.advanceTimersByTimeAsync(1_000);
    expect(api.dispatch.mock.calls.map(([attempt]) => attempt.operation))
      .toEqual(['upsertResumeEntry']);

    api.readConditional.mockResolvedValue({
      kind: 'complete', accepted: landed, etag: '"r3"',
    });
    await coordinator.refreshConditionalAndReconcile!(id);
    finishReads();
    await choosing;
    await vi.advanceTimersByTimeAsync(0);

    expect(api.dispatch).toHaveBeenCalledOnce();
    expect(store.recordFor(id)?.pending).toEqual([]);
    expect(store.saveStateFor(id)).toBe('saved');
    expect(store.recordFor(id)?.accepted.document.content.projects?.entries)
      .toEqual(landed.document.content.projects?.entries);
    expect(onLinked).toHaveBeenCalledWith({
      sectionKey: 'projects', entryId: OTHER,
    });
  });
});

describe('an entry whose project was deleted elsewhere', () => {
  it('rebases a field edit onto the unlinked entry, so the resent entry '
    + 'carries no project id', async () => {
    const { api, coordinator, id, store } = queued(withEntries([linked()]), {
      kind: 'entryField',
      sectionKey: 'projects',
      entryId: ENTRY,
      path: 'subtitle',
      value: { present: true, value: 'Lead' },
    });
    const unlinked = withEntries([{ id: ENTRY, title: 'Shop site' }], '2');
    api.dispatch
      .mockResolvedValueOnce({
        kind: 'stale', status: 412,
        winner: { document: unlinked.document, revision: unlinked.revision },
      })
      .mockResolvedValueOnce({
        kind: 'complete', status: 200,
        accepted: withEntries([
          { id: ENTRY, title: 'Shop site', subtitle: 'Lead' },
        ], '3'),
      });

    await coordinator.flush(id);

    const bodies = api.dispatch.mock.calls.map(([attempt]) =>
      JSON.parse(attempt.payload.utf8));
    expect(bodies[0].entry.projectId).toBe(PROJECT);
    expect(bodies[1].entry).toEqual({
      id: ENTRY, title: 'Shop site', subtitle: 'Lead',
    });
    expect(store.recordFor(id)?.current.document.content.projects?.entries)
      .toEqual([{ id: ENTRY, title: 'Shop site', subtitle: 'Lead' }]);
  });
});

describe('project link responses', () => {
  const headers = {
    'Cache-Control': 'no-store, no-transform',
    'Content-Type': 'application/json',
  };

  it.each([
    [404, 'project_not_found'],
    [409, 'project_already_linked'],
  ])('reads %i %s as a refusal', async (status, code) => {
    const accepted = withEntries([]);
    const command = capture(accepted, {
      kind: 'projectLink', projectId: PROJECT,
    });
    const api = createResumeApi(async () => new Response(JSON.stringify({
      error: { code, message: 'fixed' },
    }), { status, headers }));

    expect(await api.dispatch(
      freezeAttempt(command, accepted, runtime(['key-1'])), 'csrf-1',
    )).toEqual({ kind: 'rejected', code });
  });
});
