<script setup lang="ts">
import type { Section } from '@aboutme/schema';
import type { Component } from 'vue';
import { computed, nextTick, ref, toRaw, watch } from 'vue';

import type { ResumeEditorActions } from '../../../composables/useResumeEditor';
import type { EntryFieldPath } from '../../../editor/commands';
import type { Presence } from '../../../editor/types';
import CertificateEntryFields from './entries/CertificateEntryFields.vue';
import CustomEntryFields from './entries/CustomEntryFields.vue';
import EducationEntryFields from './entries/EducationEntryFields.vue';
import LanguageEntryFields from './entries/LanguageEntryFields.vue';
import ProfileEntryFields from './entries/ProfileEntryFields.vue';
import ProjectEntryFields from './entries/ProjectEntryFields.vue';
import SkillEntryFields from './entries/SkillEntryFields.vue';
import WorkEntryFields from './entries/WorkEntryFields.vue';
import EntryCard from '../EntryCard.vue';
import InspectorPanel from '../InspectorPanel.vue';
import ConfirmDialog from '../../app/ConfirmDialog.vue';
import StatusBanner from '../../app/StatusBanner.vue';
import { Button } from '../../ui/button';
import type { FieldIntent } from './fieldIntent';

const props = defineProps<{
  readonly actions: ResumeEditorActions;
  readonly section: Section;
  readonly sectionKey: string;
}>();

interface DeleteTarget {
  readonly entry: Section['entries'][number];
  readonly index: number;
  readonly sectionType: Section['sectionType'];
}

const deleteTarget = ref<DeleteTarget>();
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
    status.value = 'Entry changed. Reopen delete confirmation.';
    return;
  }
  props.actions.edit({
    kind: 'entryDelete',
    sectionKey: props.sectionKey,
    entryId: target.entry.id,
  });
  closeDelete();
}

const status = ref('');

function sameEntry(
  current: Section['entries'][number] | undefined,
  captured: Section['entries'][number],
): boolean {
  return current !== undefined
    && JSON.stringify(current) === JSON.stringify(captured);
}

function deleteLabel(target: DeleteTarget): string {
  return entryLabel(target.entry, target.index);
}

function entryLabel(
  value: Section['entries'][number],
  index: number,
): string {
  const entry = value as unknown as Record<string, unknown>;
  for (const field of ['jobTitle', 'degree', 'name', 'title'] as const) {
    const value = entry[field];
    if (typeof value === 'string' && value !== '') return value;
  }
  return `Entry ${index + 1}`;
}

function sectionHeading(section: Section): string {
  if (section.displayName !== undefined && section.displayName.trim() !== '') {
    return section.displayName;
  }
  const labels: Readonly<Record<Section['sectionType'], string>> = {
    profile: 'Summary',
    work: 'Experience',
    education: 'Education',
    skill: 'Skills',
    language: 'Languages',
    certificate: 'Certifications',
    project: 'Projects',
    custom: 'Custom section',
  };
  return labels[section.sectionType];
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
  const element = root.value?.$el
    ?? (root.value as unknown as HTMLElement | undefined);
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
  ) { return undefined; }
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
      return 'This value is too long.';
    case 'max_items':
    case 'maxItems':
      return 'There are too many entries in this section.';
    case 'rich-text-byte-length':
    case 'rich_text_byte_length':
      return 'Rich text is too long.';
    case 'date-range-order':
    case 'date_range_order':
      return 'Start date must not be after end date.';
    case 'format':
      return 'Enter a value in the required format.';
    default:
      return 'This value needs attention.';
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
      <Button
        data-action="add-entry"
        size="sm"
        @click="add"
      >
        Add entry
      </Button>
    </template>
    <p data-section-id-text>
      {{ sectionKey }}
    </p>
    <StatusBanner
      v-if="issues.length > 0"
      ref="issueSummary"
      kind="error"
      aria-label="Section issues"
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
      :title="entryLabel(entry, index)"
      @delete="openDelete(entry, index)"
      @move-down="reorder(entry.id, 1)"
      @move-up="reorder(entry.id, -1)"
      @toggle-hidden="toggleHidden(entry.id, entry.isHidden)"
    >
      <p
        class="text-xs text-muted-foreground"
        data-entry-id-text
      >
        {{ entry.id }}
      </p>
      <component
        :is="selectedComponent"
        :entry="entry"
        @field="edit(entry.id, $event.path, $event.intent)"
      />
    </EntryCard>
    <ConfirmDialog
      v-if="deleteTarget !== undefined"
      :open="true"
      title="Delete entry"
      :description="`Delete ${deleteLabel(deleteTarget)}?`"
      confirm-label="Delete"
      destructive
      confirm-action="confirm-delete-entry"
      cancel-action="cancel-delete-entry"
      @confirm="confirmDelete"
      @cancel="closeDelete"
    />
    <p
      v-if="status !== ''"
      role="status"
    >
      {{ status }}
    </p>
  </InspectorPanel>
</template>
