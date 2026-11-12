<script setup lang="ts">
import type { Section } from '@aboutme/schema';
import type { Component } from 'vue';
import { computed, nextTick, ref, toRaw, watch } from 'vue';
import LocaleToggle from '@/components/app/LocaleToggle.vue';
import { editorShellCopy } from '@/i18n/editor-shell';
import { editorFieldsCopy } from '@/i18n/editor-fields';

import type { ResumeEditorActions } from '../../../composables/useResumeEditor';
import type { EntryFieldPath } from '../../../editor/commands';
import type { Presence } from '../../../editor/types';
import CertificateEntryFields from './entries/CertificateEntryFields.vue';
import CustomEntryFields from './entries/CustomEntryFields.vue';
import EducationEntryFields from './entries/EducationEntryFields.vue';
import LanguageEntryFields from './entries/LanguageEntryFields.vue';
import ProfileEntryFields from './entries/ProfileEntryFields.vue';
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
  linkedEntry,
  linkedProjectIds,
  projectPageLanguage,
  revealsAcrossResumes,
  type EntryLocation,
} from '../../../editor/projectLinks';
import { projectName } from '../../projects/projectDocument';
import ProjectEntryMenu from './ProjectEntryMenu.vue';
import { createProjectLibrary } from './projectLibrary';

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
const linking = ref<{ commandId: string; projectId: string } | null>(null);
const linkFailed = ref(false);
/** The project waiting for the owner's privacy answer. */
const asking = ref<string | null>(null);
const current = computed(() => props.actions.record.value?.current);
const linkedProjects = computed(() => current.value === undefined
  ? new Set<string>()
  : linkedProjectIds(current.value.document));
const queuedRefreshes = computed(() => {
  const record = props.actions.record.value;
  if (record === undefined) return new Set<string>();
  const items = record.attempt === null
    ? record.pending
    : [record.attempt.queueItem, ...record.pending];
  return new Set(items.flatMap((item) =>
    item.kind === 'projectRefresh' ? [item.entryId] : []));
});
const linkStatus = computed(() => {
  if (linking.value !== null) return linkCopy.value.adding;
  return linkFailed.value ? linkCopy.value.notAdded : '';
});

watch(() => [props.sectionKey, isProject.value] as const, ([, project]) => {
  linkFailed.value = false;
  if (project) void library.load();
}, { immediate: true });

/**
 * Asks first when this published resume would be the second published one
 * to show the project (docs/design/projects.md "Privacy"). The account's
 * other resumes are read only then.
 */
async function choose(projectId: string): Promise<void> {
  const record = props.actions.record.value;
  const others = (library.find(projectId)?.linkedResumeIds ?? [])
    .filter((id) => id !== record?.accepted.metadata.id);
  if (record?.accepted.metadata.live === true && others.length > 0) {
    const published = await library.published();
    if (revealsAcrossResumes(
      record.accepted.metadata.id, others, published, true,
    )) {
      asking.value = projectId;
      return;
    }
  }
  link(projectId);
}

function addAnyway(): void {
  const projectId = asking.value;
  asking.value = null;
  if (projectId !== null) link(projectId);
}

function link(projectId: string): void {
  linkFailed.value = false;
  const result = props.actions.edit({ kind: 'projectLink', projectId });
  if (result.kind === 'enqueued') {
    linking.value = { commandId: result.command.id, projectId };
  }
}

/** Where the link landed once it left the queue, or `missing` if nowhere. */
function linkOutcome(): EntryLocation | 'missing' | null {
  const pending = linking.value;
  const record = props.actions.record.value;
  if (pending === null || record === undefined) return null;
  const queued = record.attempt?.queueItem.id === pending.commandId
    || record.pending.some((item) => item.id === pending.commandId);
  if (queued) return null;
  return linkedEntry(record.current.document, pending.projectId) ?? 'missing';
}

// The server chooses the new entry's id, so it is selected only once the
// link has left the queue and the resume shows where it landed.
watch(linkOutcome, (outcome) => {
  if (outcome === null) return;
  linking.value = null;
  void library.load();
  if (outcome === 'missing') linkFailed.value = true;
  else emit('selectEntry', outcome);
});

function refresh(entryId: string): void {
  props.actions.edit({
    kind: 'projectRefresh',
    sectionKey: props.sectionKey,
    entryId,
  });
}

interface EntryLink {
  readonly projectId: string;
  readonly name: string;
  /** The default language's name when the project page falls back to it. */
  readonly fallbackLanguage: string | null;
}

/**
 * Each linked entry's project. Until the library loads, the entry's own
 * label stands in for the project name and no fallback is known.
 */
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
  // A deleted entry's unfinished date can no longer be saved.
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
        :busy="linking !== null"
        :add-label="copy.addEntry"
        :copy="linkCopy"
        @blank="add"
        @link="choose"
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
        :refreshing="queuedRefreshes.has(entry.id)"
        :copy="linkCopy"
        :cancel-label="copy.cancel"
        @refresh="refresh(entry.id)"
      />
      <component
        :is="selectedComponent"
        :entry="entry"
        @field="edit(entry.id, $event.path, $event.intent)"
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
    <!-- docs/design/projects.md "Privacy": Cancel takes the first focus. -->
    <ConfirmDialog
      v-if="asking !== null"
      :open="true"
      :title="linkCopy.sharedPublic"
      :description="linkCopy.sharedReader"
      :confirm-label="linkCopy.addAnyway"
      :cancel-label="copy.cancel"
      destructive
      confirm-action="confirm-project-privacy"
      cancel-action="cancel-project-privacy"
      @confirm="addAnyway"
      @cancel="asking = null"
    />
  </InspectorPanel>
</template>
