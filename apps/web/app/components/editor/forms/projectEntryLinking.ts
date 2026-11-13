import type { ProjectEntry } from '@aboutme/schema';
import { computed, ref, watch, type ComputedRef, type Ref } from 'vue';

import {
  createProjectsApi,
  sendWithCsrf,
  type ProjectAuth,
  type ProjectsApi,
} from '../../../composables/projectsApi';
import { useAuth } from '../../../composables/useAuth';
import type {
  EditorContinuation,
  ResumeEditorActions,
} from '../../../composables/useResumeEditor';
import type { EntryLocation } from '../../../editor/projectLinks';
import { canonicalJSON, PROJECT_LIMIT } from '../../projects/projectDocument';
import {
  canSaveEntry,
  projectFromEntry,
} from '../../projects/projectFromEntry';
import type { ProjectLibrary } from './projectLibrary';
import { stillQueued, type ProjectLinking } from './projectLinking';

export type EntryActionStatus
  = | 'linking'
    | 'not-linked'
    | 'saving'
    | 'saved'
    | 'saved-not-linked'
    | 'save-failed'
    | 'save-invalid'
    | 'limit';

export interface EntryAction {
  readonly entryId: string;
  readonly status: EntryActionStatus;
  readonly projectId?: string;
}

export interface ProjectEntryLinking {
  readonly action: Ref<EntryAction | null>;
  readonly busy: ComputedRef<boolean>;
  readonly atLimit: ComputedRef<boolean>;
  link(entryId: string, projectId: string): Promise<void>;
  save(entryId: string): Promise<void>;
}

interface QueuedEntryLink {
  readonly location: EntryLocation;
  readonly projectId: string;
  readonly commandId: string;
  readonly saved: boolean;
}

function createKeys(uuid: () => string) {
  let pending: { fingerprint: string; key: string } | null = null;
  return {
    keyFor(request: unknown): string {
      const fingerprint = canonicalJSON(request);
      if (pending?.fingerprint !== fingerprint) {
        pending = { fingerprint, key: uuid() };
      }
      return pending.key;
    },
    settle(): void {
      pending = null;
    },
  };
}

const RETRYABLE: ReadonlySet<string> = new Set([
  'rate-limited', 'unavailable', 'csrf-rejected',
]);

const NOT_SENT = { kind: 'failed', failure: 'session-lost' } as const;

export function useProjectEntryLinking(options: {
  readonly actions: ResumeEditorActions;
  readonly sectionKey: () => string;
  readonly library: ProjectLibrary;
  readonly linking: ProjectLinking;
  readonly language: () => string;
  readonly onLinked: (location: EntryLocation) => void;
  readonly api?: Pick<ProjectsApi, 'create'>;
  readonly auth?: ProjectAuth;
  readonly uuid?: () => string;
}): ProjectEntryLinking {
  const { actions, library, linking } = options;
  const api = options.api ?? createProjectsApi();
  const auth = options.auth ?? useAuth();
  const keys = createKeys(options.uuid ?? (() => crypto.randomUUID()));
  const action = ref<EntryAction | null>(null);
  const working = ref(false);
  const queued = ref<QueuedEntryLink | null>(null);
  const limitHit = ref(false);

  const busy = computed(() => working.value || queued.value !== null);
  const atLimit = computed(() => limitHit.value
    || (library.view.value.kind === 'ready'
      && library.view.value.items.length >= PROJECT_LIMIT));
  watch(() => library.view.value, (view) => {
    if (view.kind === 'ready') limitHit.value = false;
  });

  function entryAt(location: EntryLocation): ProjectEntry | undefined {
    const section = actions.record.value?.current.document
      .content[location.sectionKey];
    if (section?.sectionType !== 'project') return undefined;
    const entry = section.entries.find((candidate) =>
      candidate.id === location.entryId);
    return entry?.projectId === undefined ? entry : undefined;
  }

  function start(entryId: string): {
    readonly location: EntryLocation;
    readonly entry: ProjectEntry;
    readonly run: EditorContinuation;
  } | null {
    if (working.value || queued.value !== null || linking.busy.value) {
      return null;
    }
    const location = { sectionKey: options.sectionKey(), entryId };
    const entry = entryAt(location);
    const run = actions.continuation();
    return entry === undefined || run === null
      ? null
      : { location, entry, run };
  }

  function abandon(): void {
    working.value = false;
    action.value = null;
  }

  function finish(
    location: EntryLocation,
    status: EntryActionStatus,
    projectId?: string,
  ): void {
    working.value = false;
    action.value = {
      entryId: location.entryId,
      status,
      ...(projectId === undefined ? {} : { projectId }),
    };
  }

  function enqueue(
    run: EditorContinuation,
    location: EntryLocation,
    projectId: string,
    saved: boolean,
  ): void {
    if (!run.current()) {
      abandon();
      return;
    }
    const failed = saved ? 'saved-not-linked' : 'not-linked';
    if (entryAt(location) === undefined) {
      finish(location, failed, projectId);
      return;
    }
    const result = actions.edit({
      kind: 'projectLinkEntry', ...location, projectId,
    });
    if (result.kind !== 'enqueued') {
      finish(location, failed, projectId);
      return;
    }
    finish(location, saved ? 'saving' : 'linking', projectId);
    queued.value = {
      location, projectId, commandId: result.command.id, saved,
    };
  }

  watch(() => queued.value !== null
    && !stillQueued(actions, queued.value.commandId), (left) => {
    const pending = queued.value;
    if (!left || pending === null) return;
    queued.value = null;
    void library.load();
    const section = actions.record.value?.current.document
      .content[pending.location.sectionKey];
    const landed = section?.entries.some((entry) =>
      entry.id === pending.location.entryId
      && 'projectId' in entry && entry.projectId === pending.projectId);
    const { entryId } = pending.location;
    const projectId = pending.projectId;
    if (landed) {
      action.value = pending.saved
        ? { entryId, projectId, status: 'saved' }
        : null;
      options.onLinked(pending.location);
      return;
    }
    action.value = {
      entryId,
      projectId,
      status: pending.saved ? 'saved-not-linked' : 'not-linked',
    };
  });

  async function link(entryId: string, projectId: string): Promise<void> {
    const started = start(entryId);
    if (started === null) return;
    const { location, run } = started;
    working.value = true;
    action.value = null;
    const decided = await linking.decide(
      projectId,
      () => enqueue(run, location, projectId, false),
      abandon,
    );
    if (!decided) abandon();
  }

  async function save(entryId: string): Promise<void> {
    const started = start(entryId);
    if (started === null) return;
    const { location, entry, run } = started;
    if (!canSaveEntry(entry)) {
      finish(location, 'save-invalid');
      return;
    }
    working.value = true;
    action.value = { entryId, status: 'saving' };
    const body = projectFromEntry(entry, options.language());
    const key = keys.keyFor({ ownerId: run.ownerId, body });
    const result = await sendWithCsrf(auth, run.ownerId, (csrfToken) =>
      run.current()
        ? api.create(body, key, csrfToken)
        : Promise.resolve(NOT_SENT));
    const failure = result.kind === 'failed' ? result.failure : null;
    if (failure === null || !RETRYABLE.has(failure)) keys.settle();
    if (!run.current()) {
      abandon();
      return;
    }
    if (result.kind !== 'ok') {
      if (failure === 'limit') {
        limitHit.value = true;
        void library.load();
      }
      finish(location, failure === 'limit'
        ? 'limit'
        : failure === 'invalid' ? 'save-invalid' : 'save-failed');
      return;
    }
    const projectId = result.value.id;
    void library.load();
    const decided = await linking.decide(
      projectId,
      () => enqueue(run, location, projectId, true),
      () => {
        if (run.current()) finish(location, 'saved', projectId);
        else abandon();
      },
    );
    if (!decided) finish(location, 'saved-not-linked', projectId);
  }

  return { action, busy, atLimit, link, save };
}
