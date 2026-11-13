<script setup lang="ts">
import type { Section } from '@aboutme/schema';
import type { Component } from 'vue';
import { computed, nextTick, ref, toRaw, watch } from 'vue';
import LocaleToggle from '@/components/app/LocaleToggle.vue';
import { editorShellCopy } from '@/i18n/editor-shell';
import { editorFieldsCopy } from '@/i18n/editor-fields';
import { projectsCopy } from '@/i18n/projects';

import type { ResumeEditorActions } from '../../../composables/useResumeEditor';
import type { EntryFieldPath } from '../../../editor/commands';
import type { Presence } from '../../../editor/types';
import CertificateEntryFields from './entries/CertificateEntryFields.vue';
import CustomEntryFields from './entries/CustomEntryFields.vue';
import EducationEntryFields from './entries/EducationEntryFields.vue';
import LanguageEntryFields from './entries/LanguageEntryFields.vue';
import ProfileEntryFields from './entries/ProfileEntryFields.vue';
import ProjectEntryActions from './entries/ProjectEntryActions.vue';
import ProjectEntryFields from './entries/ProjectEntryFields.vue';
import ProjectLinkNote from './entries/ProjectLinkNote.vue';
import SkillEntryFields from './entries/SkillEntryFields.vue';
import WorkEntryFields from './entries/WorkEntryFields.vue';
import EntryCard from '../EntryCard.vue';
import { defaultSectionNames, entryLabel } from '../sectionTypes';
import InspectorPanel from '../InspectorPanel.vue';
import ConfirmDialog from '../../app/ConfirmDialog.vue';
import StatusBanner from '../../app/StatusBanner.vue';
import { Button } from '../../ui/button';
import type { FieldIntent } from './fieldIntent';
import { useFieldDrafts } from '../../../composables/useFieldDrafts';
import {
  linkedProjectIds,
  projectPageLanguage,
  type EntryLocation,
} from '../../../editor/projectLinks';
import {
  PROJECT_LIMIT,
  projectName,
  TEXT_LIMITS,
} from '../../projects/projectDocument';
import { projectLanguage } from '../../projects/projectFromEntry';
import ProjectDuplicateDialog from './ProjectDuplicateDialog.vue';
import ProjectEntryMenu from './ProjectEntryMenu.vue';
import {
  useProjectEntryLinking,
  type EntryActionStatus,
} from './projectEntryLinking';
import { createProjectLibrary } from './projectLibrary';
import { useProjectLinking } from './projectLinking';

const props = defineProps<{
  readonly actions: ResumeEditorActions;
  readonly section: Section;
  readonly sectionKey: string;
  readonly selectedEntryId?: string;
}>();
const emit = defineEmits<{ selectEntry: [location: EntryLocation] }>();
const { locale } = useLocale();
const copy = computed(() => editorFieldsCopy[locale.value].section);
const linkCopy = computed(() => editorFieldsCopy[locale.value].projectLinks);

interface DeleteTarget {
  readonly entry: Section['entries'][number];
  readonly index: number;
  readonly sectionType: Section['sectionType'];
}

const deleteTarget = ref<DeleteTarget>();
const drafts = useFieldDrafts();
const issues = computed(() =>
  Object.values(props.actions.record.value?.issues ?? {}).flat(),
);
const root = ref<{ $el?: HTMLElement }>();
const issueSummary = ref<{ $el?: HTMLElement }>();
const selectedComponent = computed<Component>(() => {
  switch (props.section.sectionType) {
    case 'profile':
      return ProfileEntryFields;
    case 'work':
      return WorkEntryFields;
    case 'education':
      return EducationEntryFields;
    case 'skill':
      return SkillEntryFields;
    case 'language':
      return LanguageEntryFields;
    case 'certificate':
      return CertificateEntryFields;
    case 'project':
      return ProjectEntryFields;
    case 'custom':
      return CustomEntryFields;
    default:
      return assertNever(props.section);
  }
});

watch(
  () => issues.value.map((issue) => `${issue.path}:${issue.code}`).join('|'),
  async (next, previous) => {
    if (next === '' || next === previous) return;
    await nextTick();
    issueSummary.value?.$el?.focus();
  },
);

const isProject = computed(() => props.section.sectionType === 'project');
const library = createProjectLibrary();
const current = computed(() => props.actions.record.value?.current);
const linkedProjects = computed(() => current.value === undefined
  ? new Set<string>()
  : linkedProjectIds(current.value.document));
const linking = useProjectLinking({
  actions: props.actions,
  sectionKey: () => props.sectionKey,
  library,
  onLinked: (location) => emit('selectEntry', location),
});
const linkStatus = computed(() => {
  switch (linking.status.value) {
    case 'adding': return linkCopy.value.adding;
    case 'not-added': return linkCopy.value.notAdded;
    case 'changed-elsewhere': return linkCopy.value.changedElsewhere;
    default: return '';
  }
});

const entryLinking = useProjectEntryLinking({
  actions: props.actions,
  sectionKey: () => props.sectionKey,
  library,
  linking,
  language: () => projectLanguage(current.value?.metadata.lng, locale.value),
  onLinked: (location) => emit('selectEntry', location),
});
const anyBusy = computed(() => linking.busy.value || entryLinking.busy.value);
const limitLine = computed(() =>
  projectsCopy[locale.value].limitReached(PROJECT_LIMIT));

function entryStatusText(status: EntryActionStatus): string {
  const text = linkCopy.value;
  switch (status) {
    case 'linking': return text.linkingEntry;
    case 'not-linked': return text.entryNotLinked;
    case 'saving': return text.saving;
    case 'saved': return text.saved;
    case 'saved-not-linked': return text.savedNotLinked;
    case 'save-failed': return text.saveFailed;
    case 'save-invalid': return text.saveNeedsTitle(TEXT_LIMITS.name);
    case 'limit': return limitLine.value;
  }
}

function entryAction(entryId: string) {
  const action = entryLinking.action.value;
  if (action?.entryId !== entryId) {
    return { status: '', savedProjectId: null };
  }
  return {
    status: entryStatusText(action.status),
    savedProjectId: action.status === 'saved-not-linked'
      ? action.projectId ?? null
      : null,
  };
}

watch(() => [props.sectionKey, isProject.value] as const, ([, project]) => {
  linking.reset();
  if (project) void library.load();
}, { immediate: true });

interface EntryLink {
  readonly projectId: string;
  readonly name: string;
  readonly fallbackLanguage: string | null;
}

const entryLinks = computed(() => {
  const links = new Map<string, EntryLink>();
  if (!isProject.value) return links;
  props.section.entries.forEach((entry, index) => {
    if (!('projectId' in entry) || entry.projectId === undefined) return;
    const project = library.find(entry.projectId);
    const page = project === undefined
      ? undefined
      : projectPageLanguage(current.value?.metadata.lng, project);
    links.set(entry.id, {
      projectId: entry.projectId,
      name: project === undefined
        ? entryLabel(entry, index, locale.value)
        : projectName(project),
      fallbackLanguage: page?.fallback === true
        ? linkCopy.value.languageName(page.key)
        : null,
    });
  });
  return links;
});

function add(): void {
  props.actions.edit({
    kind: 'entryUpsert',
    sectionKey: props.sectionKey,
    entry: { id: props.actions.createEntityId() },
  });
}

function edit(
  entryId: string,
  path: EntryFieldPath,
  intent: FieldIntent<unknown>,
): void {
  const value: Presence
    = intent.kind === 'unset'
      ? { present: false }
      : { present: true, value: intent.value };
  props.actions.edit({
    kind: 'entryField',
    sectionKey: props.sectionKey,
    entryId,
    path,
    value,
  });
}

function toggleHidden(entryId: string, isHidden: boolean | undefined): void {
  props.actions.edit({
    kind: 'entryField',
    sectionKey: props.sectionKey,
    entryId,
    path: 'isHidden',
    value: { present: true, value: !isHidden },
  });
}

function openDelete(entry: Section['entries'][number], index: number): void {
  deleteTarget.value = {
    entry: structuredClone(toRaw(entry)),
    index,
    sectionType: props.section.sectionType,
  };
}

function reorder(entryId: string, direction: -1 | 1): void {
  const ids = props.section.entries.map((entry) => entry.id);
  const index = ids.indexOf(entryId);
  const next = index + direction;
  if (index < 0 || next < 0 || next >= ids.length) return;
  const [moved] = ids.splice(index, 1);
  if (moved === undefined) return;
  ids.splice(next, 0, moved);
  props.actions.edit({
    kind: 'entryReorder',
    sectionKey: props.sectionKey,
    entryIds: ids,
  });
}

function closeDelete(): void {
  deleteTarget.value = undefined;
}

function confirmDelete(): void {
  const target = deleteTarget.value;
  if (target === undefined) return;
  const current = props.section.entries.find(
    (entry) => entry.id === target.entry.id,
  );
  if (
    props.section.sectionType !== target.sectionType
    || !sameEntry(current, target.entry)
  ) {
    closeDelete();
    status.value = 'entryChanged';
    return;
  }
  props.actions.edit({
    kind: 'entryDelete',
    sectionKey: props.sectionKey,
    entryId: target.entry.id,
  });
  drafts?.clearPrefix(`dates:${target.entry.id}`);
  closeDelete();
}

const status = ref<'entryChanged'>();

function sameEntry(
  current: Section['entries'][number] | undefined,
  captured: Section['entries'][number],
): boolean {
  return (
    current !== undefined
    && JSON.stringify(current) === JSON.stringify(captured)
  );
}

function deleteLabel(target: DeleteTarget): string {
  return entryLabel(target.entry, target.index, locale.value);
}

function sectionHeading(section: Section): string {
  if (section.displayName !== undefined && section.displayName.trim() !== '') {
    return section.displayName;
  }
  return defaultSectionNames[section.sectionType];
}

function focusIssue(path: string): void {
  const location = issueLocation(path);
  if (location === undefined) return;
  const entrySelector = `[data-entry-id="${location.entryId}"]`;
  const fieldSelector = `[data-entry-field="${location.path}"]`;
  const selector
    = location.path === 'isHidden'
      ? `[data-entry-id="${location.entryId}"] [data-action="toggle-hidden"]`
      : [
          `${entrySelector} ${fieldSelector} input`,
          `${entrySelector} ${fieldSelector} select`,
          `${entrySelector} ${fieldSelector} [contenteditable="true"]`,
        ].join(', ');
  const element
    = root.value?.$el ?? (root.value as unknown as HTMLElement | undefined);
  element?.querySelector<HTMLElement>(selector)?.focus();
}

function issueLocation(
  path: string,
): { readonly entryId: string; readonly path: EntryFieldPath } | undefined {
  const prefix = `^content\\.${escapeRegExp(props.sectionKey)}`;
  const suffix = '\\.entries\\[(\\d+)\\]\\.([A-Za-z]+)$';
  const match = new RegExp(prefix + suffix).exec(path);
  if (match === null) return undefined;
  const index = Number(match[1]);
  const field = match[2];
  if (
    !Number.isSafeInteger(index)
    || field === undefined
    || !isEntryFieldPath(field)
  ) {
    return undefined;
  }
  const entry = props.section.entries[index];
  return entry === undefined ? undefined : { entryId: entry.id, path: field };
}

function isEntryFieldPath(path: string): path is EntryFieldPath {
  return [
    'isHidden',
    'text',
    'jobTitle',
    'employer',
    'employerLink',
    'city',
    'country',
    'dates',
    'description',
    'degree',
    'school',
    'schoolLink',
    'name',
    'level',
    'infoHtml',
    'title',
    'titleLink',
    'issuer',
    'date',
    'link',
    'subtitle',
  ].includes(path);
}

function escapeRegExp(value: string): string {
  return value.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
}

function messageForCode(code: string): string {
  switch (code) {
    case 'max_length':
    case 'maxLength':
      return copy.value.tooLong;
    case 'max_items':
    case 'maxItems':
      return copy.value.tooMany;
    case 'rich-text-byte-length':
    case 'rich_text_byte_length':
      return copy.value.richTextTooLong;
    case 'date-range-order':
    case 'date_range_order':
      return editorFieldsCopy[locale.value].dates.order;
    case 'format':
      return copy.value.format;
    default:
      return copy.value.generic;
  }
}

function assertNever(value: never): never {
  throw new Error(`Unsupported section: ${String(value)}`);
}
</script>

<template>
  <InspectorPanel
    ref="root"
    :data-section-key="sectionKey"
    :title="sectionHeading(section)"
    :title-id="`${sectionKey}-title`"
    tabindex="-1"
  >
    <template #actions>
      <ProjectEntryMenu
        v-if="isProject"
        :library="library.view.value"
        :linked="linkedProjects"
        :busy="anyBusy"
        :add-label="copy.addEntry"
        :copy="linkCopy"
        @blank="add"
        @link="linking.choose"
        @retry="library.load()"
      />
      <Button
        v-else
        data-action="add-entry"
        size="sm"
        @click="add"
      >
        {{ copy.addEntry }}
      </Button>
    </template>
    <p
      v-if="isProject"
      class="text-sm text-muted-foreground empty:hidden"
      data-testid="project-link-status"
      role="status"
    >
      {{ linkStatus }}
    </p>
    <StatusBanner
      v-if="issues.length > 0"
      ref="issueSummary"
      kind="error"
      :aria-label="copy.issues"
      tabindex="-1"
    >
      <template
        v-for="issue in issues"
        :key="`${issue.path}:${issue.code}`"
      >
        <Button
          v-if="issueLocation(issue.path) !== undefined"
          variant="link"
          type="button"
          :data-issue="issue.path"
          @click="focusIssue(issue.path)"
        >
          {{ messageForCode(issue.code) }}
        </Button>
        <span v-else>{{ messageForCode(issue.code) }}</span>
      </template>
    </StatusBanner>
    <EntryCard
      v-for="(entry, index) in section.entries"
      :key="entry.id"
      :count="section.entries.length"
      :entry-id="entry.id"
      :hidden="entry.isHidden ?? false"
      :index="index"
      :title="entryLabel(entry, index, locale)"
      :project-name="entryLinks.get(entry.id)?.name"
      :selected="entry.id === selectedEntryId"
      @delete="openDelete(entry, index)"
      @move-down="reorder(entry.id, 1)"
      @move-up="reorder(entry.id, -1)"
      @toggle-hidden="toggleHidden(entry.id, entry.isHidden)"
    >
      <ProjectLinkNote
        v-if="entryLinks.has(entry.id)"
        class="mb-4"
        :project-id="entryLinks.get(entry.id)!.projectId"
        :name="entryLinks.get(entry.id)!.name"
        :fallback-language="entryLinks.get(entry.id)!.fallbackLanguage"
        :refreshing="linking.refreshingEntries.value.has(entry.id)"
        :copy="linkCopy"
        :cancel-label="copy.cancel"
        @refresh="linking.refresh(entry.id)"
      />
      <component
        :is="selectedComponent"
        :entry="entry"
        @field="edit(entry.id, $event.path, $event.intent)"
      />
      <ProjectEntryActions
        v-if="isProject"
        :linked="entryLinks.has(entry.id)"
        :library="library.view.value"
        :shown="linkedProjects"
        :busy="anyBusy"
        :at-limit="entryLinking.atLimit.value"
        :limit-line="limitLine"
        :status="entryAction(entry.id).status"
        :saved-project-id="entryAction(entry.id).savedProjectId"
        :copy="linkCopy"
        @link="entryLinking.link(entry.id, $event)"
        @save="entryLinking.save(entry.id)"
        @retry="library.load()"
      />
    </EntryCard>
    <ConfirmDialog
      v-if="deleteTarget !== undefined"
      :open="true"
      :title="copy.deleteEntry"
      :description="copy.deleteDescription(deleteLabel(deleteTarget))"
      :confirm-label="copy.delete"
      :cancel-label="copy.cancel"
      destructive
      confirm-action="confirm-delete-entry"
      cancel-action="cancel-delete-entry"
      @confirm="confirmDelete"
      @cancel="closeDelete"
    >
      <template #header-actions>
        <LocaleToggle
          :label="editorShellCopy[locale].localeLabel"
          @pointerdown.prevent
        />
      </template>
    </ConfirmDialog>
    <p
      v-if="status !== undefined"
      role="status"
    >
      {{ copy.entryChanged }}
    </p>
    <ConfirmDialog
      v-if="linking.asking.value !== null"
      :open="true"
      :title="linkCopy.sharedPublic"
      :description="linkCopy.sharedReader"
      :confirm-label="linkCopy.addAnyway"
      :cancel-label="copy.cancel"
      destructive
      confirm-action="confirm-project-privacy"
      cancel-action="cancel-project-privacy"
      @confirm="linking.addAnyway()"
      @cancel="linking.cancelAsk()"
    />
    <ProjectDuplicateDialog
      v-if="linking.duplicate.value !== null"
      :open="true"
      :title="linkCopy.fromProjects"
      :question="linkCopy.duplicateQuestion(linking.duplicate.value.title)"
      :link-label="linkCopy.linkThatEntry"
      :add-label="linkCopy.addNewEntry"
      :cancel-label="copy.cancel"
      @link="linking.linkThatEntry()"
      @add="linking.addNewEntry()"
      @cancel="linking.cancelDuplicate()"
    />
  </InspectorPanel>
</template>
