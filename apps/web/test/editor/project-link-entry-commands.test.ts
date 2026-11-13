import type { Resume, Section } from '@aboutme/schema';
import { CURRENT_VERSION } from '@aboutme/schema/released';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import {
  captureCommand,
  coalescePending,
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
import type { AcceptedResume, EditorRuntime } from '../../app/editor/types';
import { acceptedFixture } from './fixture';
import { openEditor } from './held-write-editor';

// Linking an existing, unlinked project entry (docs/design/projects.md
// "Selection"): an atomic command through the editor's write queue that adds
// only the reference and keeps every field of the entry.

const PROJECT = '0190f000-0000-7000-8000-0000000000c1';
const OTHER_PROJECT = '0190f000-0000-7000-8000-0000000000c2';
const ENTRY = '0190f000-0000-7000-8000-0000000000e1';
const NEIGHBOUR = '0190f000-0000-7000-8000-0000000000e2';

type Entry = Section['entries'][number];

const written = (overrides: Partial<Entry> = {}): Entry => ({
  id: ENTRY,
  title: 'Shop site',
  subtitle: 'Lead',
  link: 'https://github.com/ada/shop',
  dates: { start: { y: 2024 }, end: null, present: true },
  description: '<p>Built it.</p>',
  isHidden: true,
  ...overrides,
} as Entry);

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

const linkEntry = (
  entryId = ENTRY,
  projectId = PROJECT,
): AtomicCommandIntent => ({
  kind: 'projectLinkEntry', sectionKey: 'projects', entryId, projectId,
});

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

const entriesOf = (document: Resume) => document.content.projects?.entries;

describe('linking an existing entry', () => {
  it('captures the unlinked entry as its base, and locally adds only the '
    + 'reference', () => {
    const accepted = withEntries([written()]);
    const command = capture(accepted, linkEntry());

    expect(command.base.target).toEqual({ present: false });
    expect(command.intended?.target).toEqual({
      present: true, value: PROJECT,
    });
    expect(entriesOf(replayCommand(accepted, command).document))
      .toEqual([{ ...written(), projectId: PROJECT }]);
  });

  it('is sent as a POST of the project and the entry with the resume '
    + 'revision', async () => {
    const accepted = withEntries([written()], '7');
    const command = capture(accepted, linkEntry());

    const request = requestFromAttempt(
      freezeAttempt(command, accepted, runtime(['key-1'])),
      'csrf-1',
    );

    expect(request.method).toBe('POST');
    expect(new URL(request.url, 'http://local').pathname)
      .toBe('/api/v1/resumes/resume-1/project-links');
    expect(request.headers.get('If-Match')).toBe('"r7"');
    expect(request.headers.get('Idempotency-Key')).toBe('key-1');
    expect(request.headers.get('X-CSRF-Token')).toBe('csrf-1');
    expect(request.headers.get('X-Resume-Schema-Version'))
      .toBe(String(CURRENT_VERSION));
    expect(await request.json())
      .toEqual({ projectId: PROJECT, entryId: ENTRY });
  });

  it('is satisfied by the link, safe over text changed elsewhere, and in '
    + 'conflict when the entry is gone or linked to another project', () => {
    const command = capture(withEntries([written()]), linkEntry());

    expect(reconcileCommand(command, withEntries(
      [written({ projectId: PROJECT } as Partial<Entry>)], '2',
    )).kind).toBe('satisfied');
    expect(reconcileCommand(command, withEntries(
      [written({ title: 'Edited elsewhere', isHidden: false })], '2',
    )).kind).toBe('safe-base');
    expect(reconcileCommand(command, withEntries([], '2')).kind)
      .toBe('conflict');
    expect(reconcileCommand(command, withEntries(
      [written({ projectId: OTHER_PROJECT } as Partial<Entry>)], '2',
    )).kind).toBe('conflict');
  });

  it('never merges with a queued command', () => {
    const accepted = withEntries([written()]);
    const first = capture(accepted, linkEntry());
    const second = capture(accepted, linkEntry());

    expect(coalescePending([first], second)).toEqual([first, second]);
  });

  it('reads 404 entry_not_found as a refusal', async () => {
    const accepted = withEntries([written()]);
    const command = capture(accepted, linkEntry());
    const api = createResumeApi(async () => new Response(JSON.stringify({
      error: { code: 'entry_not_found', message: 'fixed' },
    }), {
      status: 404,
      headers: {
        'Cache-Control': 'no-store, no-transform',
        'Content-Type': 'application/json',
      },
    }));

    expect(await api.dispatch(
      freezeAttempt(command, accepted, runtime(['key-1'])), 'csrf-1',
    )).toEqual({ kind: 'rejected', code: 'entry_not_found' });
  });
});

describe('a link of an existing entry through the write queue', () => {
  beforeEach(() => {
    vi.useFakeTimers();
    vi.setSystemTime(0);
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  const neighbour = { id: NEIGHBOUR, title: 'Ops tool' } as Entry;
  const open = () => openEditor(withEntries([written(), neighbour]));
  const typed = (value: string): AtomicCommandIntent => ({
    kind: 'entryField',
    sectionKey: 'projects',
    entryId: NEIGHBOUR,
    path: 'subtitle',
    value: { present: true, value },
  });

  it('is sent at once, keeps every field of the entry and leaves the queue',
    async () => {
      const context = open();
      const link = context.edit(linkEntry());

      await vi.advanceTimersByTimeAsync(0);

      expect(context.server.sentFor(link)).toEqual(['complete']);
      expect(entriesOf(context.server.stored.document)).toEqual([
        { ...written(), projectId: PROJECT }, neighbour,
      ]);
      expect(entriesOf(context.record().current.document))
        .toEqual(entriesOf(context.server.stored.document));
      expect(context.record()).toMatchObject({ attempt: null, pending: [] });
      expect(context.store.saveStateFor(context.id)).toBe('saved');
    });

  it('is rebased with a new key after a 412 whose winner changed only the '
    + 'text, and keeps that text', async () => {
    const context = open();
    context.server.otherWriter(() => withEntries([
      written({ title: 'Renamed elsewhere' }), neighbour,
    ]).document);
    const link = context.edit(linkEntry());

    await context.idle();

    const attempts = context.server.sent
      .filter(({ attempt }) => attempt.id === link)
      .map(({ attempt }) => attempt);
    expect(context.server.sentFor(link)).toEqual(['stale', 'complete']);
    expect(attempts.map((attempt) => attempt.ifMatch))
      .toEqual(['"r1"', '"r2"']);
    expect(attempts[1]!.idempotencyKey).not.toBe(attempts[0]!.idempotencyKey);
    expect(attempts[1]!.payload).toEqual(attempts[0]!.payload);
    expect(entriesOf(context.record().current.document)![0]).toEqual({
      ...written({ title: 'Renamed elsewhere' }), projectId: PROJECT,
    });
    expect(context.record()).toMatchObject({
      attempt: null, pending: [], conflicts: [],
    });
  });

  it.each([
    ['the entry was deleted elsewhere', () => [neighbour]],
    ['the entry was linked to another project elsewhere', () => [
      written({ projectId: OTHER_PROJECT } as Partial<Entry>), neighbour,
    ]],
  ])('leaves the queue after a 412 when %s, and the edits behind it are '
    + 'sent', async (_case, winner) => {
    const context = open();
    context.server.otherWriter(() => withEntries(winner()).document);
    const link = context.edit(linkEntry());
    const later = context.edit(typed('Staff'));

    await context.idle();

    expect(context.server.sentFor(link)).toEqual(['stale']);
    expect(context.server.sentFor(later)).toEqual(['complete']);
    expect(context.record()).toMatchObject({
      attempt: null, pending: [], conflicts: [],
    });
    expect(entriesOf(context.record().current.document))
      .toEqual(entriesOf(context.server.stored.document));
    expect(context.store.saveStateFor(context.id)).toBe('saved');
  });

  it('resends the same request with the same key when its outcome is '
    + 'unknown, and lands once', async () => {
    const context = open();
    context.server.loseNextResponse({ applied: true });
    const link = context.edit(linkEntry());

    await context.idle();

    const attempts = context.server.sent
      .filter(({ attempt }) => attempt.id === link)
      .map(({ attempt }) => attempt);
    expect(context.server.sentFor(link)).toEqual(['unknown', 'complete']);
    expect(new Set(attempts.map((attempt) => attempt.idempotencyKey)).size)
      .toBe(1);
    expect(attempts.map((attempt) => attempt.ifMatch))
      .toEqual(['"r1"', '"r1"']);
    expect(context.server.stored.revision).toBe(parseRevision('2'));
    expect(context.record()).toMatchObject({ attempt: null, pending: [] });
    expect(entriesOf(context.record().current.document)![0])
      .toEqual({ ...written(), projectId: PROJECT });
  });

  it.each([
    ['an entry the server no longer has', 'entry_not_found'],
    ['a project the resume already shows', 'project_already_linked'],
    ['a project that is gone', 'project_not_found'],
  ] as const)('drops a link refused for %s, rereads, and sends the edits '
    + 'behind it', async (_case, code) => {
    const context = open();
    context.server.failNextWrite({ kind: 'rejected', code });
    const link = context.edit(linkEntry());
    const later = context.edit(typed('Staff'));

    await context.idle();

    expect(context.server.sentFor(link)).toEqual(['rejected']);
    expect(context.server.sentFor(later)).toEqual(['complete']);
    expect(context.server.reads).toBeGreaterThanOrEqual(1);
    expect(context.record()).toMatchObject({
      attempt: null, pending: [], issues: {},
    });
    expect(entriesOf(context.record().current.document)![0])
      .toEqual(written());
  });
});
