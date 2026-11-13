import type { Resume } from '@aboutme/schema';
import { computed, ref, watch, type ComputedRef, type Ref } from 'vue';

import type { ResumeEditorActions } from '../../../composables/useResumeEditor';
import {
  linkedEntry,
  revealsAcrossResumes,
  type EntryLocation,
} from '../../../editor/projectLinks';
import { takeProjectOutcome } from '../../../editor/projectOutcomes';
import type { Project } from '../../projects/projectDocument';
import type { ProjectLibrary } from './projectLibrary';

export type ProjectLinkStatus
  = 'adding' | 'not-added' | 'changed-elsewhere' | null;

interface Choice {
  readonly projectId: string;
  /** An unlinked entry to link instead of adding one. */
  readonly entry?: EntryLocation;
}

interface QueuedLink extends Choice {
  readonly commandId: string;
}

/** The question asked before showing a project on a second public resume. */
interface PrivacyAsk {
  readonly proceed: () => void;
  readonly cancel?: () => void;
}

/** An unlinked entry that may already be the chosen project. */
export interface DuplicateAsk {
  readonly projectId: string;
  readonly entry: EntryLocation;
  readonly title: string;
}

export interface ProjectLinking {
  readonly status: ComputedRef<ProjectLinkStatus>;
  /** Busy deciding or waiting for a link; the menu offers no project. */
  readonly busy: ComputedRef<boolean>;
  /** The link waiting for the owner's privacy answer. */
  readonly asking: Ref<PrivacyAsk | null>;
  /** The choice waiting for Link that entry or Add a new entry. */
  readonly duplicate: Ref<DuplicateAsk | null>;
  readonly refreshingEntries: ComputedRef<ReadonlySet<string>>;
  choose(projectId: string): Promise<void>;
  linkThatEntry(): Promise<void>;
  addNewEntry(): Promise<void>;
  cancelDuplicate(): void;
  /**
   * Runs `proceed` now, or `proceed` or `cancel` once the owner answers the
   * privacy question. False when it could not decide: the editor has no
   * record, or another link is being decided or sent.
   */
  decide(
    projectId: string,
    proceed: () => void,
    cancel?: () => void,
  ): Promise<boolean>;
  addAnyway(): void;
  cancelAsk(): void;
  refresh(entryId: string): void;
  reset(): void;
}

/** Whether `commandId` is still queued or held by the editor's queue. */
export function stillQueued(
  actions: ResumeEditorActions,
  commandId: string,
): boolean {
  const record = actions.record.value;
  return record !== undefined
    && (record.attempt?.queueItem.id === commandId
      || record.pending.some((item) => item.id === commandId));
}

function sameText(value: string | undefined): string {
  return (value ?? '').trim().normalize('NFC').toLowerCase();
}

/**
 * The first unlinked project entry of `document` with one of the
 * project's names as its title, ignoring case and surrounding space, or
 * with one of its links (docs/design/projects.md "Selection").
 */
export function duplicateEntry(
  document: Resume,
  project: Project,
): Omit<DuplicateAsk, 'projectId'> | undefined {
  const names = new Set(Object.values(project.text)
    .map((text) => sameText(text.name))
    .filter((name) => name !== ''));
  const projectLinks = [project.repositoryUrl, project.demoUrl]
    .filter((link): link is string => typeof link === 'string' && link !== '');
  const { main, sidebar } = document.customization.layout.sections;
  const keys = [...new Set([
    ...main, ...sidebar, ...Object.keys(document.content),
  ])];
  for (const sectionKey of keys) {
    const section = document.content[sectionKey];
    if (section?.sectionType !== 'project') continue;
    const entry = section.entries.find((candidate) =>
      candidate.projectId === undefined
      && (names.has(sameText(candidate.title))
        || (candidate.link !== undefined
          && projectLinks.includes(candidate.link.trim()))));
    if (entry !== undefined) {
      return {
        entry: { sectionKey, entryId: entry.id },
        title: entry.title?.trim() ?? '',
      };
    }
  }
  return undefined;
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
  const asking = ref<PrivacyAsk | null>(null);
  const duplicate = ref<DuplicateAsk | null>(null);
  const refreshes = ref<readonly string[]>([]);
  const outcome = ref<'not-added' | 'changed-elsewhere' | null>(null);

  const queued = (commandId: string): boolean =>
    stillQueued(actions, commandId);
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
   * when the person acts, from the account's resumes and the project's
   * links read again then; a read that fails asks. The queue's own resends
   * of the same link do not decide again, and no answer is kept.
   */
  async function decide(
    projectId: string,
    proceed: () => void,
    cancel?: () => void,
  ): Promise<boolean> {
    const record = actions.record.value;
    if (record === undefined || busy.value) return false;
    outcome.value = null;
    deciding.value = true;
    const [published, linkedIds] = await Promise.all([
      library.published(), library.linkedTo(projectId),
    ]);
    deciding.value = false;
    const resumeId = record.accepted.metadata.id;
    if (revealsAcrossResumes(resumeId, linkedIds, published)) {
      asking.value = { proceed, ...(cancel === undefined ? {} : { cancel }) };
      return true;
    }
    proceed();
    return true;
  }

  function link(choice: Choice): void {
    const result = actions.edit(choice.entry === undefined
      ? { kind: 'projectLink', projectId: choice.projectId }
      : {
          kind: 'projectLinkEntry',
          sectionKey: choice.entry.sectionKey,
          entryId: choice.entry.entryId,
          projectId: choice.projectId,
        });
    if (result.kind === 'enqueued') {
      linking.value = { ...choice, commandId: result.command.id };
    }
  }

  const decideAndLink = async (choice: Choice): Promise<void> => {
    await decide(choice.projectId, () => link(choice));
  };

  /**
   * A project the resume already has as an unlinked entry asks first
   * whether to link that entry or add a new one, so it is not shown twice
   * by accident.
   */
  async function choose(projectId: string): Promise<void> {
    const record = actions.record.value;
    if (record === undefined || busy.value) return;
    outcome.value = null;
    const project = library.find(projectId);
    const match = project === undefined
      ? undefined
      : duplicateEntry(record.current.document, project);
    if (match !== undefined) {
      duplicate.value = { projectId, ...match };
      return;
    }
    await decideAndLink({ projectId });
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

  function takeDuplicate(): DuplicateAsk | null {
    const ask = duplicate.value;
    duplicate.value = null;
    return ask;
  }

  return {
    status,
    busy,
    asking,
    duplicate,
    refreshingEntries,
    choose,
    async linkThatEntry() {
      const ask = takeDuplicate();
      if (ask !== null) {
        await decideAndLink({ projectId: ask.projectId, entry: ask.entry });
      }
    },
    async addNewEntry() {
      const ask = takeDuplicate();
      if (ask !== null) await decideAndLink({ projectId: ask.projectId });
    },
    cancelDuplicate() {
      duplicate.value = null;
    },
    decide,
    addAnyway() {
      const ask = asking.value;
      asking.value = null;
      ask?.proceed();
    },
    cancelAsk() {
      const ask = asking.value;
      asking.value = null;
      ask?.cancel?.();
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
