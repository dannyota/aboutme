import type { ProjectEntry } from '@aboutme/schema';
import { computed, ref, watch, type ComputedRef, type Ref } from 'vue';

import {
  createProjectsApi,
  sendWithCsrf,
  type ProjectAuth,
  type ProjectsApi,
} from '../../../composables/projectsApi';
import { useAuth } from '../../../composables/useAuth';
import type { ResumeEditorActions } from '../../../composables/useResumeEditor';
import type { EntryLocation } from '../../../editor/projectLinks';
import {
  canonicalJSON,
  PROJECT_LIMIT,
  type CreateProjectRequest,
} from '../../projects/projectDocument';
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
  /** The project linked or saved; a saved project stays in the library. */
  readonly projectId?: string;
}

export interface ProjectEntryLinking {
  /** The latest action on an entry of this section, and how it went. */
  readonly action: Ref<EntryAction | null>;
  readonly busy: ComputedRef<boolean>;
  /** Save to my projects gives way to the limit line. */
  readonly atLimit: ComputedRef<boolean>;
  link(entryId: string, projectId: string): Promise<void>;
  save(entryId: string): Promise<void>;
  reset(): void;
}

interface QueuedEntryLink {
  readonly location: EntryLocation;
  readonly projectId: string;
  readonly commandId: string;
  readonly saved: boolean;
}

/** A key per create intent, kept only while that same request may be sent. */
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

/**
 * An unlinked project entry's Link to a project and Save to my projects
 * (docs/design/ui/projects.md "In the resume editor"). Either one links
 * the entry through the editor's write queue, keeping all its text; Save
 * first creates the library project from the entry. A link that fails
 * after the create leaves the project in the library, and nothing is
 * retried on the person's behalf.
 */
export function useProjectEntryLinking(options: {
  readonly actions: ResumeEditorActions;
  readonly sectionKey: () => string;
  readonly library: ProjectLibrary;
  readonly linking: ProjectLinking;
  /** The language a project made from an entry is written in. */
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
  const creating = ref(false);
  const queued = ref<QueuedEntryLink | null>(null);
  const limitHit = ref(false);

  const busy = computed(() => creating.value || queued.value !== null);
  const atLimit = computed(() => limitHit.value
    || (library.view.value.kind === 'ready'
      && library.view.value.items.length >= PROJECT_LIMIT));
  // A refused create reads the list again, which then tells the count.
  watch(() => library.view.value, (view) => {
    if (view.kind === 'ready') limitHit.value = false;
  });

  function entryOf(entryId: string): ProjectEntry | undefined {
    const section = actions.record.value?.current.document
      .content[options.sectionKey()];
    if (section?.sectionType !== 'project') return undefined;
    const entry = section.entries.find((candidate) => candidate.id === entryId);
    return entry?.projectId === undefined ? entry : undefined;
  }

  const blocked = () => busy.value || linking.busy.value;

  function enqueue(entryId: string, projectId: string, saved: boolean): void {
    const location = { sectionKey: options.sectionKey(), entryId };
    const result = actions.edit({
      kind: 'projectLinkEntry', ...location, projectId,
    });
    if (result.kind !== 'enqueued') {
      action.value = {
        entryId, projectId, status: saved ? 'saved-not-linked' : 'not-linked',
      };
      return;
    }
    action.value = { entryId, projectId, status: saved ? 'saving' : 'linking' };
    queued.value = {
      location, projectId, commandId: result.command.id, saved,
    };
  }

  // The link left the queue: landed, or refused or dropped unapplied.
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

  async function create(
    entryId: string,
    body: CreateProjectRequest,
  ): Promise<void> {
    const ownerId = auth.user.value?.id;
    if (ownerId === undefined || entryOf(entryId) === undefined) {
      action.value = { entryId, status: 'save-failed' };
      return;
    }
    const key = keys.keyFor({ ownerId, body });
    creating.value = true;
    action.value = { entryId, status: 'saving' };
    const result = await sendWithCsrf(auth, ownerId, (csrfToken) =>
      api.create(body, key, csrfToken));
    creating.value = false;
    if (result.kind === 'ok') {
      keys.settle();
      void library.load();
      if (entryOf(entryId) === undefined) {
        action.value = {
          entryId, projectId: result.value.id, status: 'saved-not-linked',
        };
        return;
      }
      enqueue(entryId, result.value.id, true);
      return;
    }
    const failure = result.kind === 'failed' ? result.failure : 'unavailable';
    // Nothing ran, or the outcome is unknown: Save again sends the same
    // request with the same key, and the server answers it at most once.
    if (failure !== 'rate-limited' && failure !== 'unavailable'
      && failure !== 'csrf-rejected') {
      keys.settle();
    }
    if (failure === 'limit') {
      limitHit.value = true;
      void library.load();
    }
    action.value = {
      entryId,
      status: failure === 'limit'
        ? 'limit'
        : failure === 'invalid' ? 'save-invalid' : 'save-failed',
    };
  }

  return {
    action,
    busy,
    atLimit,
    async link(entryId, projectId) {
      if (blocked() || entryOf(entryId) === undefined) return;
      action.value = null;
      await linking.decide(projectId, () => {
        if (entryOf(entryId) === undefined) {
          action.value = { entryId, projectId, status: 'not-linked' };
          return;
        }
        enqueue(entryId, projectId, false);
      });
    },
    async save(entryId) {
      const entry = entryOf(entryId);
      if (blocked() || entry === undefined) return;
      if (!canSaveEntry(entry)) {
        action.value = { entryId, status: 'save-invalid' };
        return;
      }
      action.value = null;
      // The body is taken when the person acts, from the text they see.
      const body = projectFromEntry(entry, options.language());
      await linking.decide(null, () => void create(entryId, body));
    },
    reset() {
      if (!busy.value) action.value = null;
    },
  };
}
