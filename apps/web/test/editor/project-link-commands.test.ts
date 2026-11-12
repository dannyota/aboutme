import type { Section } from '@aboutme/schema';
import { CURRENT_VERSION } from '@aboutme/schema/released';
import { createPinia, setActivePinia } from 'pinia';
import { computed } from 'vue';
import { afterEach, describe, expect, it, vi } from 'vitest';

import { createMutationCoordinator } from '../../app/editor/coordinator';
import {
  captureCommand,
  replayCommand,
  type AtomicCommandIntent,
} from '../../app/editor/commands';
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
  dependencyIdsForNewCommand, nextSequence, useResumeStore,
} from '../../app/stores/resumes';
import { acceptedFixture } from './fixture';
import { fakeApi, ok, project, resumeSummary } from '../projects/fixtures';

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
    list: vi.fn().mockResolvedValue({
      kind: 'ready', items: [resumeSummary(accepted.metadata.id)],
    }),
  };
  const projects = fakeApi();
  projects.read.mockResolvedValue(ok(project()));
  const coordinator = createMutationCoordinator({
    api: api as never,
    store,
    auth,
    projects,
    runtime: runtime(['attempt-1', 'attempt-2', 'attempt-3']),
  });
  const id = accepted.metadata.id;
  return { store, api, projects, coordinator, id };
}

function enqueue(
  store: ReturnType<typeof useResumeStore>,
  id: string,
  intent: AtomicCommandIntent,
): void {
  const record = store.recordFor(id)!;
  store.enqueue(id, captureCommand(record.current, {
    resumeId: id, ownerId: 'owner-1', sequence: nextSequence(record),
    dependencyIds: dependencyIdsForNewCommand(record), intent,
  }, runtime([`command-${nextSequence(record)}`])));
}

const editTitle = (title: string): AtomicCommandIntent => ({
  kind: 'entryField', sectionKey: 'projects', entryId: ENTRY,
  path: 'title', value: { present: true, value: title },
});

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

  it('repeats once on a revision mismatch with the new revision and a '
    + 'fresh key', async () => {
    const { api, coordinator, id, store } = queued(withEntries([]), {
      kind: 'projectLink', projectId: PROJECT,
    });
    const winner = withEntries([], '4');
    api.dispatch
      .mockResolvedValueOnce({
        kind: 'stale', status: 412,
        winner: { document: winner.document, revision: winner.revision },
      })
      .mockResolvedValueOnce({
        kind: 'complete', status: 200, accepted: withEntries([linked()], '5'),
      });

    await coordinator.flush(id);

    const attempts = api.dispatch.mock.calls.map(([attempt]) => attempt);
    expect(attempts.map((attempt) => attempt.ifMatch))
      .toEqual(['"r1"', '"r4"']);
    expect(attempts[1].idempotencyKey).not.toBe(attempts[0].idempotencyKey);
    expect(store.recordFor(id)?.attempt).toBeNull();
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

  it('asks again before retrying a link that became public elsewhere',
    async () => {
      const { api, projects, coordinator, store, id } = queued(
        withEntries([]), { kind: 'projectLink', projectId: PROJECT },
      );
      api.list.mockResolvedValueOnce({
        kind: 'ready', items: [resumeSummary(id)],
      }).mockResolvedValue({
        kind: 'ready', items: [
          resumeSummary(id, '2', 'Current', { live: true }),
          resumeSummary('other', '1', 'Other', { live: true }),
        ],
      });
      projects.read.mockResolvedValue(ok(project({
        linkedResumeIds: ['other'],
      })));
      const winner = withEntries([], '2');
      api.dispatch.mockResolvedValueOnce({
        kind: 'stale', status: 412,
        winner: { document: winner.document, revision: winner.revision },
      });
      api.read.mockResolvedValue({ kind: 'complete', accepted: winner });

      await coordinator.flush(id);

      expect(api.dispatch).toHaveBeenCalledOnce();
      expect(store.recordFor(id)?.conflicts[0]).toMatchObject({
        kind: 'project-privacy', command: { kind: 'projectLink' },
      });
      await coordinator.acceptLatest(id, store.recordFor(id)!.conflicts[0]!.id);
      expect(api.dispatch).toHaveBeenCalledOnce();
      expect(store.recordFor(id)?.pending).toEqual([]);
    });

  it('checks privacy before manually retrying a refused link attempt',
    async () => {
      const { api, projects, coordinator, store, id } = queued(
        withEntries([]), { kind: 'projectLink', projectId: PROJECT },
      );
      api.dispatch.mockResolvedValueOnce({
        kind: 'rate-limited', retryAfterMs: null,
      });
      await coordinator.flush(id);
      api.list.mockResolvedValue({ kind: 'ready', items: [
        resumeSummary(id, '2', 'Current', { live: true }),
        resumeSummary('other', '1', 'Other', { live: true }),
      ] });
      projects.read.mockResolvedValue(ok(project({
        linkedResumeIds: ['other'],
      })));

      await coordinator.retry(id, store.recordFor(id)!.attempt!.command.id);

      expect(api.dispatch).toHaveBeenCalledOnce();
      expect(store.recordFor(id)?.conflicts[0]?.kind).toBe('project-privacy');
    });

  it('sends a privacy-confirmed replacement once with a fresh revision',
    async () => {
      const { api, projects, coordinator, store, id } = queued(
        withEntries([]), { kind: 'projectLink', projectId: PROJECT },
      );
      api.list.mockResolvedValue({
        kind: 'ready', items: [
          resumeSummary(id, '2', 'Current', { live: true }),
          resumeSummary('other', '1', 'Other', { live: true }),
        ],
      });
      projects.read.mockResolvedValue(ok(project({
        linkedResumeIds: ['other'],
      })));
      const latest = withEntries([], '2');
      api.read.mockResolvedValue({ kind: 'complete', accepted: latest });
      api.dispatch.mockResolvedValueOnce({
        kind: 'complete', status: 200, accepted: withEntries([linked()], '3'),
      });
      await coordinator.flush(id);
      expect(api.dispatch).not.toHaveBeenCalled();

      await coordinator.applyMine(id, store.recordFor(id)!.conflicts[0]!.id,
        { kind: 'field' });
      await coordinator.flush(id);

      expect(api.dispatch).toHaveBeenCalledOnce();
      expect(api.dispatch.mock.calls[0]![0].ifMatch).toBe('"r2"');
      expect(store.recordFor(id)?.conflicts).toEqual([]);
      expect(store.recordFor(id)?.pending).toEqual([]);
    });

  it('asks before sending a queued link when the fresh privacy read fails',
    async () => {
      const { api, projects, coordinator, store, id } = queued(
        withEntries([]), { kind: 'projectLink', projectId: PROJECT },
      );
      projects.read.mockResolvedValue({
        kind: 'failed', failure: 'unavailable',
      });

      await coordinator.flush(id);

      expect(api.dispatch).not.toHaveBeenCalled();
      expect(store.recordFor(id)?.conflicts[0]?.kind).toBe('project-privacy');
      await coordinator.acceptLatest(id, store.recordFor(id)!.conflicts[0]!.id);
      expect(api.dispatch).not.toHaveBeenCalled();
    });

  it('sends edits behind a link satisfied by the 412 winner', async () => {
    const { store, api, coordinator, id } = queued(withEntries([]), {
      kind: 'projectLink', projectId: PROJECT,
    });
    enqueue(store, id, {
      kind: 'personalField', path: 'headline',
      value: { present: true, value: 'Typed after linking' },
    });
    const winner = withEntries([linked()], '2');
    api.dispatch.mockResolvedValueOnce({
      kind: 'stale', status: 412,
      winner: { document: winner.document, revision: winner.revision },
    }).mockResolvedValueOnce({
      kind: 'complete', status: 200, accepted: {
        ...winner, revision: parseRevision('3'), document: {
          ...winner.document, personalDetails: {
            ...winner.document.personalDetails, headline: 'Typed after linking',
          },
        },
      },
    });
    api.read.mockResolvedValue({ kind: 'complete', accepted: winner });

    await coordinator.flush(id);

    expect(api.dispatch.mock.calls.map(([attempt]) => attempt.ifMatch))
      .toEqual(['"r1"', '"r2"']);
    expect(store.recordFor(id)?.completeReadRequired).toBe(false);
    expect(store.recordFor(id)?.pending).toEqual([]);
  });

  it.each([
    { kind: 'rejected', code: 'project_not_found' },
    { kind: 'rejected', code: 'project_already_linked' },
    { kind: 'validation-rejected', issues: [] },
  ])('sends edits after a stale link is refused: $kind $code',
    async (refusal) => {
      const { store, api, coordinator, id } = queued(withEntries([]), {
        kind: 'projectLink', projectId: PROJECT,
      });
      enqueue(store, id, {
        kind: 'personalField', path: 'headline',
        value: { present: true, value: 'Queued' },
      });
      const winner = withEntries([], '2');
      api.dispatch.mockResolvedValueOnce({
        kind: 'stale', status: 412,
        winner: { document: winner.document, revision: winner.revision },
      }).mockResolvedValueOnce(refusal).mockResolvedValueOnce({
        kind: 'complete', status: 200,
        accepted: { ...winner, revision: parseRevision('3') },
      });
      api.read.mockResolvedValue({ kind: 'complete', accepted: winner });

      await coordinator.flush(id);

      expect(api.dispatch.mock.calls.map(([attempt]) => attempt.ifMatch))
        .toEqual(['"r1"', '"r2"', '"r2"']);
      expect(store.recordFor(id)?.completeReadRequired).toBe(false);
      expect(store.recordFor(id)?.pending).toEqual([]);
    });

  it('keeps trailing edits behind the barrier when a refusal reread fails',
    async () => {
      const { store, api, coordinator, id } = queued(withEntries([]), {
        kind: 'projectLink', projectId: PROJECT,
      });
      enqueue(store, id, {
        kind: 'personalField', path: 'headline',
        value: { present: true, value: 'Queued' },
      });
      api.dispatch.mockResolvedValueOnce({
        kind: 'rejected', code: 'project_not_found',
      });
      api.read.mockResolvedValueOnce({ kind: 'unavailable' });

      await coordinator.flush(id);

      expect(api.dispatch).toHaveBeenCalledOnce();
      expect(store.recordFor(id)?.completeReadRequired).toBe(true);
      expect(store.recordFor(id)?.pending).toHaveLength(1);
      api.read.mockResolvedValueOnce({
        kind: 'complete', accepted: withEntries([], '1'),
      });
      api.dispatch.mockResolvedValueOnce({
        kind: 'complete', status: 200, accepted: withEntries([], '2'),
      });
      await coordinator.completeRead(id);
      await coordinator.flush(id);
      expect(api.dispatch).toHaveBeenCalledTimes(2);
      expect(store.recordFor(id)?.pending).toEqual([]);
    });

  it('preserves typing on the same entry before and after a queued link',
    async () => {
      const initial = withEntries([{ id: ENTRY, title: 'Old' }]);
      const { store, api, coordinator, id } = queued(
        initial, editTitle('Before'),
      );
      enqueue(store, id, { kind: 'projectLink', projectId: PROJECT });
      enqueue(store, id, editTitle('After'));
      const added = linked({ id: 'new-entry' });
      api.dispatch.mockResolvedValueOnce({
        kind: 'complete', status: 200,
        accepted: withEntries([{ id: ENTRY, title: 'Before' }], '2'),
      }).mockResolvedValueOnce({
        kind: 'complete', status: 200,
        accepted: withEntries([{ id: ENTRY, title: 'Before' }, added], '3'),
      }).mockResolvedValueOnce({
        kind: 'complete', status: 200,
        accepted: withEntries([{ id: ENTRY, title: 'After' }, added], '4'),
      });

      await coordinator.flush(id);

      const attempts = api.dispatch.mock.calls.map(([attempt]) => attempt);
      expect(attempts.map((attempt) => attempt.ifMatch))
        .toEqual(['"r1"', '"r2"', '"r3"']);
      expect(JSON.parse(attempts[0].payload.utf8).entry.title).toBe('Before');
      expect(JSON.parse(attempts[1].payload.utf8))
        .toEqual({ projectId: PROJECT });
      expect(JSON.parse(attempts[2].payload.utf8).entry.title).toBe('After');
      expect(store.recordFor(id)?.pending).toEqual([]);
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
  it('captures the link and prefill fields without changing text', () => {
    const accepted = withEntries([linked()]);
    const command = capture(accepted, {
      kind: 'projectRefresh', sectionKey: 'projects', entryId: ENTRY,
    });

    expect(command.base.target).toEqual({
      present: true, value: {
        projectId: { present: true, value: PROJECT },
        title: { present: true, value: 'Shop site' },
        subtitle: { present: false }, link: { present: false },
        dates: { present: false }, description: { present: false },
      },
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

  it.each([
    { title: 'Edited elsewhere' },
    { subtitle: 'New role' },
    { link: 'https://example.com' },
    { dates: { start: { y: 2025, m: 1 }, end: null, present: true } },
    { description: '<p><strong>New text</strong></p>' },
  ])('keeps changed prefill text on a 412 without resending: %j',
    async (changed) => {
      const { store, api, coordinator, id } = queued(withEntries([linked()]), {
        kind: 'projectRefresh', sectionKey: 'projects', entryId: ENTRY,
      });
      enqueue(store, id, {
        kind: 'personalField', path: 'headline',
        value: { present: true, value: 'Queued' },
      });
      const winner = withEntries([linked(changed as Partial<Entry>)], '2');
      api.dispatch.mockResolvedValueOnce({
        kind: 'stale', status: 412,
        winner: { document: winner.document, revision: winner.revision },
      }).mockResolvedValueOnce({
        kind: 'complete', status: 200,
        accepted: { ...winner, revision: parseRevision('3') },
      });
      api.read.mockResolvedValue({ kind: 'complete', accepted: winner });

      await coordinator.flush(id);

      expect(api.dispatch.mock.calls.map(([attempt]) => attempt.operation))
        .toEqual(['refreshResumeProjectEntry', 'updateResumePersonalDetails']);
      expect(store.recordFor(id)?.current.document.content.projects?.entries)
        .toEqual(winner.document.content.projects?.entries);
      expect(store.recordFor(id)?.completeReadRequired).toBe(false);
      expect(store.recordFor(id)?.pending).toEqual([]);
      expect(store.recordFor(id)?.conflicts[0]).toMatchObject({
        subject: 'atomic', command: { kind: 'projectRefresh' },
      });
    });

  it('compares dates by value and rich text exactly, including formatting',
    () => {
      const before = withEntries([linked({
        dates: { start: { y: 2025, m: 1 }, end: null, present: true },
        description: '<p>Text</p>',
      } as Partial<Entry>)]);
      const command = capture(before, {
        kind: 'projectRefresh', sectionKey: 'projects', entryId: ENTRY,
      });
      const same = structuredClone(before);
      expect(reconcileCommand(command, same).kind).toBe('safe-base');
      const formatting = withEntries([linked({
        dates: { start: { y: 2025, m: 1 }, end: null, present: true },
        description: '<p><strong>Text</strong></p>',
      } as Partial<Entry>)], '2');
      expect(reconcileCommand(command, formatting).kind).toBe('conflict');
      const changedDates = withEntries([linked({
        dates: { start: { y: 2025 }, end: null, present: true },
        description: '<p>Text</p>',
      } as Partial<Entry>)], '2');
      expect(reconcileCommand(command, changedDates).kind).toBe('conflict');
      expect(reconcileCommand(command, withEntries([linked()], '2')).kind)
        .toBe('conflict');
    });

  it('rebases once when only fields outside the prefill changed', async () => {
    const { api, coordinator, id } = queued(withEntries([linked()]), {
      kind: 'projectRefresh', sectionKey: 'projects', entryId: ENTRY,
    });
    const winner = withEntries([linked({ isHidden: true })], '2');
    api.dispatch.mockResolvedValueOnce({
      kind: 'stale', status: 412,
      winner: { document: winner.document, revision: winner.revision },
    }).mockResolvedValueOnce({
      kind: 'complete', status: 200,
      accepted: withEntries([
        linked({ title: 'Refreshed', isHidden: true }),
      ], '3'),
    });

    await coordinator.flush(id);

    expect(api.dispatch.mock.calls.map(([attempt]) => attempt.ifMatch))
      .toEqual(['"r1"', '"r2"']);
  });

  it('preserves typing on the same entry before and after a queued refresh',
    async () => {
      const { store, api, coordinator, id } = queued(
        withEntries([linked()]), editTitle('Before'),
      );
      enqueue(store, id, {
        kind: 'projectRefresh', sectionKey: 'projects', entryId: ENTRY,
      });
      enqueue(store, id, editTitle('After'));
      api.dispatch.mockResolvedValueOnce({
        kind: 'complete', status: 200,
        accepted: withEntries([linked({ title: 'Before' })], '2'),
      }).mockResolvedValueOnce({
        kind: 'complete', status: 200,
        accepted: withEntries([
          linked({ title: 'Library', subtitle: 'Fresh' }),
        ], '3'),
      }).mockResolvedValueOnce({
        kind: 'complete', status: 200,
        accepted: withEntries([
          linked({ title: 'After', subtitle: 'Fresh' }),
        ], '4'),
      });

      await coordinator.flush(id);

      const attempts = api.dispatch.mock.calls.map(([attempt]) => attempt);
      expect(attempts.map((attempt) => attempt.operation))
        .toEqual([
          'upsertResumeEntry', 'refreshResumeProjectEntry', 'upsertResumeEntry',
        ]);
      expect(JSON.parse(attempts[2].payload.utf8).entry).toMatchObject({
        title: 'After', subtitle: 'Fresh', projectId: PROJECT,
      });
      expect(store.recordFor(id)?.pending).toEqual([]);
      expect(store.recordFor(id)?.conflicts).toEqual([]);
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
