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
  readonly entry?: EntryLocation;
}

interface QueuedLink extends Choice {
  readonly commandId: string;
}

interface PrivacyAsk {
  readonly proceed: () => void;
  readonly cancel?: () => void;
}

export interface DuplicateAsk {
  readonly projectId: string;
  readonly entry: EntryLocation;
  readonly title: string;
}

export interface ProjectLinking {
  readonly status: ComputedRef<ProjectLinkStatus>;
  readonly busy: ComputedRef<boolean>;
  readonly asking: Ref<PrivacyAsk | null>;
  readonly duplicate: Ref<DuplicateAsk | null>;
  readonly refreshingEntries: ComputedRef<ReadonlySet<string>>;
  choose(projectId: string): Promise<void>;
  linkThatEntry(): Promise<void>;
  addNewEntry(): Promise<void>;
  cancelDuplicate(): void;
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
