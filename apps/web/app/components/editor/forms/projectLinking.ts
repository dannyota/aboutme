import { computed, ref, watch, type ComputedRef, type Ref } from 'vue';

import type { ResumeEditorActions } from '../../../composables/useResumeEditor';
import {
  linkedEntry,
  revealsAcrossResumes,
  type EntryLocation,
} from '../../../editor/projectLinks';
import { takeProjectOutcome } from '../../../editor/projectOutcomes';
import type { ProjectLibrary } from './projectLibrary';

export type ProjectLinkStatus
  = 'adding' | 'not-added' | 'changed-elsewhere' | null;

interface Choice {
  readonly projectId: string;
}

interface QueuedLink extends Choice {
  readonly commandId: string;
}

export interface ProjectLinking {
  readonly status: ComputedRef<ProjectLinkStatus>;
  /** Busy deciding or waiting for a link; the menu offers no project. */
  readonly busy: ComputedRef<boolean>;
  /** The project waiting for the owner's privacy answer. */
  readonly asking: Ref<Choice | null>;
  readonly refreshingEntries: ComputedRef<ReadonlySet<string>>;
  choose(projectId: string): Promise<void>;
  addAnyway(): void;
  cancelAsk(): void;
  refresh(entryId: string): void;
  reset(): void;
}

/**
 * A project section's links and refreshes, sent through the editor's write
 * queue and followed until they leave it (docs/design/ui/projects.md "In
 * the resume editor").
 */
export function useProjectLinking(options: {
  readonly actions: ResumeEditorActions;
  readonly sectionKey: () => string;
  readonly library: ProjectLibrary;
  readonly onLinked: (location: EntryLocation) => void;
}): ProjectLinking {
  const { actions, library } = options;
  const deciding = ref(false);
  const linking = ref<QueuedLink | null>(null);
  const asking = ref<Choice | null>(null);
  const refreshes = ref<readonly string[]>([]);
  const outcome = ref<'not-added' | 'changed-elsewhere' | null>(null);

  const queued = (commandId: string): boolean => {
    const record = actions.record.value;
    return record !== undefined
      && (record.attempt?.queueItem.id === commandId
        || record.pending.some((item) => item.id === commandId));
  };
  const busy = computed(() => deciding.value || linking.value !== null);
  const status = computed<ProjectLinkStatus>(() =>
    (busy.value ? 'adding' : outcome.value));
  const refreshingEntries = computed(() => {
    const record = actions.record.value;
    if (record === undefined) return new Set<string>();
    const items = record.attempt === null
      ? record.pending
      : [record.attempt.queueItem, ...record.pending];
    return new Set(items.flatMap((item) =>
      item.kind === 'projectRefresh' ? [item.entryId] : []));
  });

  /**
   * Asks first when this resume would be the second published one to show
   * the project (docs/design/projects.md "Privacy"). It is decided once,
   * when the person chooses, from the account's resumes and the project's
   * links read again then; a read that fails asks. The queue's own resends
   * of the same link do not decide again, and no answer is kept.
   */
  async function choose(projectId: string): Promise<void> {
    const record = actions.record.value;
    if (record === undefined || busy.value) return;
    outcome.value = null;
    deciding.value = true;
    const [published, linkedIds] = await Promise.all([
      library.published(), library.linkedTo(projectId),
    ]);
    deciding.value = false;
    const resumeId = record.accepted.metadata.id;
    if (revealsAcrossResumes(resumeId, linkedIds, published)) {
      asking.value = { projectId };
      return;
    }
    link({ projectId });
  }

  function link(choice: Choice): void {
    const result = actions.edit({
      kind: 'projectLink', projectId: choice.projectId,
    });
    if (result.kind === 'enqueued') {
      linking.value = { ...choice, commandId: result.command.id };
    }
  }

  // The server chooses the new entry's id, so it is selected only once the
  // link has left the queue and the resume shows where it landed.
  watch(() => linking.value !== null && !queued(linking.value.commandId),
    (left) => {
      const pending = linking.value;
      if (!left || pending === null) return;
      linking.value = null;
      void library.load();
      const record = actions.record.value;
      const landed = record === undefined
        ? undefined
        : linkedEntry(record.current.document, pending.projectId);
      if (landed === undefined) outcome.value = 'not-added';
      else options.onLinked(landed);
    });

  watch(() => refreshes.value.filter((commandId) => !queued(commandId)),
    (left) => {
      if (left.length === 0) return;
      refreshes.value = refreshes.value.filter((id) => !left.includes(id));
      const reasons = left.map(takeProjectOutcome);
      if (reasons.includes('refresh-changed')) {
        outcome.value = 'changed-elsewhere';
      }
    });

  return {
    status,
    busy,
    asking,
    refreshingEntries,
    choose,
    addAnyway() {
      const choice = asking.value;
      asking.value = null;
      if (choice !== null) link(choice);
    },
    cancelAsk() {
      asking.value = null;
    },
    refresh(entryId) {
      outcome.value = null;
      const result = actions.edit({
        kind: 'projectRefresh', sectionKey: options.sectionKey(), entryId,
      });
      if (result.kind === 'enqueued') {
        refreshes.value = [...refreshes.value, result.command.id];
      }
    },
    reset() {
      outcome.value = null;
    },
  };
}
