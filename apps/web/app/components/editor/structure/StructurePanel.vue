<script setup lang="ts">
import type { Section } from '@aboutme/schema';
import { computed, nextTick, ref, toRaw, watch } from 'vue';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Input } from '@/components/ui/input';
import SelectField from '@/components/app/SelectField.vue';
import ConfirmDialog from '@/components/app/ConfirmDialog.vue';
import FormField from '@/components/app/FormField.vue';
import InspectorPanel from '../InspectorPanel.vue';
import StatusBanner from '@/components/app/StatusBanner.vue';

import type { ResumeEditorActions } from '../../../composables/useResumeEditor';
import type { ServerValidationIssue } from '../../../editor/attempt';
import type { AtomicEditorCommand } from '../../../editor/commands';
import type { AtomicConflictRecord } from '../../../editor/conflicts';
import EntryOrderControls from './EntryOrderControls.vue';
import SectionControls from './SectionControls.vue';
import { defaultSectionIcons, defaultSectionNames } from '../sectionTypes';
import LocaleToggle from '@/components/app/LocaleToggle.vue';
import { editorShellCopy } from '@/i18n/editor-shell';
import { editorControlsCopy } from '../../../i18n/editor-controls';

type Column = 'main' | 'sidebar';
type SectionAction = {
  readonly key: string;
  readonly sectionType: Section['sectionType'];
};
type PlacementAction = SectionAction & {
  readonly column: Column;
  readonly index: number;
};
type MetadataAction = SectionAction & {
  readonly field: 'displayName' | 'iconKey';
  readonly value: string | null;
};
type EntryReorderAction = {
  readonly entryIds: readonly string[];
  readonly sectionKey: string;
  readonly sectionType: Section['sectionType'];
};
type ReopenConflict = AtomicConflictRecord & {
  readonly command: Extract<
    AtomicEditorCommand,
    { kind: 'entryReorder' | 'structure' }
  >;
};
type DeleteTarget = SectionAction & {
  readonly placement: {
    readonly main: readonly string[];
    readonly sidebar: readonly string[];
  };
  readonly section: Section;
};

const props = defineProps<{
  readonly actions: ResumeEditorActions;
}>();
const emit = defineEmits<{ openSection: [key: string] }>();
const { locale } = useLocale();
const copy = computed(() => editorControlsCopy[locale.value].controls);

const newColumn = ref<Column>('main');
const newSectionType = ref<Section['sectionType']>('work');
// The name follows the type until the person types their own.
const newDisplayName = ref(defaultSectionNames[newSectionType.value]);
watch(newSectionType, (next, previous) => {
  if (newDisplayName.value === defaultSectionNames[previous]) {
    newDisplayName.value = defaultSectionNames[next];
  }
});
const pendingDelete = ref<DeleteTarget | null>(null);
const root = ref<{ $el?: HTMLElement } | null>(null);
type StructureStatus
  = | 'customSectionInvalidId'
    | 'sectionChanged'
    | 'sectionCreateDuplicate'
    | 'sectionMissing';
const status = ref<StructureStatus | null>(null);
const statusText = computed(() =>
  status.value === null ? '' : copy.value[status.value],
);
const record = computed(() => props.actions.record.value);
const currentDocument = computed(() => record.value?.current.document);
const placement = computed(
  () => currentDocument.value?.customization.layout.sections,
);

const sections = computed(() => {
  const current = currentDocument.value;
  const currentPlacement = placement.value;
  if (current === undefined || currentPlacement === undefined) return [];
  return (['main', 'sidebar'] as const).flatMap((column) =>
    currentPlacement[column].flatMap((key, index) => {
      const section = current.content[key];
      return section === undefined ? [] : [{ column, index, key, section }];
    }),
  );
});

const structureConflicts = computed(() =>
  (record.value?.conflicts ?? []).filter(isStructureConflict),
);

const entryOrderConflicts = computed(() =>
  (record.value?.conflicts ?? []).filter(isEntryOrderConflict),
);

const structureIssues = computed(() =>
  Object.values(record.value?.issues ?? {})
    .flat()
    .filter(isStructureIssue),
);

watch(structureIssues, (issues, previous) => {
  if (issues.length === 0 || issues === previous) return;
  void nextTick(() => focusIssue(issues[0]!));
});

function columnKeys(column: Column): readonly string[] {
  return placement.value?.[column] ?? [];
}

function sectionMatches(action: SectionAction): boolean {
  const current = currentDocument.value;
  const currentPlacement = placement.value;
  if (current === undefined || currentPlacement === undefined) return false;
  const section = current.content[action.key];
  if (section?.sectionType !== action.sectionType) return false;
  const occurrences = [
    ...currentPlacement.main,
    ...currentPlacement.sidebar,
  ].filter((key) => key === action.key).length;
  return occurrences === 1;
}

function sectionDisabled(key: string, section: Section): boolean {
  return !sectionMatches({ key, sectionType: section.sectionType });
}

function createSection(): void {
  const current = currentDocument.value;
  if (current === undefined) return;
  const custom = newSectionType.value === 'custom';
  const key = custom ? props.actions.createEntityId() : newSectionType.value;
  if (custom && !isRepositoryUuid(key)) {
    status.value = 'customSectionInvalidId';
    return;
  }
  if (current.content[key] !== undefined) {
    status.value = 'sectionCreateDuplicate';
    return;
  }
  const icon = defaultSectionIcons[newSectionType.value];
  const result = props.actions.edit({
    kind: 'structure',
    commands: [
      {
        op: 'createSection',
        key,
        sectionType: newSectionType.value,
        column: newColumn.value,
        index: columnKeys(newColumn.value).length,
        ...(newDisplayName.value.trim() === ''
          ? {}
          : { displayName: newDisplayName.value.trim() }),
        ...(icon === null ? {} : { iconKey: icon }),
      },
    ],
  });
  if (result.kind !== 'enqueued') return;
  newDisplayName.value = defaultSectionNames[newSectionType.value];
  emit('openSection', key);
}

function move(action: PlacementAction): void {
  if (!sectionMatches(action)) return;
  const source = (['main', 'sidebar'] as const).find((column) =>
    columnKeys(column).includes(action.key),
  );
  if (source === undefined || action.index < 0) return;
  const without = columnKeys(source).filter((key) => key !== action.key);
  const targetAfterRemoval
    = source === action.column ? without : columnKeys(action.column);
  const index = Math.min(action.index, targetAfterRemoval.length);
  props.actions.edit({
    kind: 'structure',
    commands: [
      { op: 'moveSection', key: action.key, column: action.column, index },
    ],
  });
}

function reorder(action: PlacementAction): void {
  if (!sectionMatches(action)) return;
  const keys = [...columnKeys(action.column)];
  const sourceIndex = keys.indexOf(action.key);
  if (sourceIndex < 0 || action.index < 0) return;
  const [moved] = keys.splice(sourceIndex, 1);
  if (moved === undefined) return;
  keys.splice(Math.min(action.index, keys.length), 0, moved);
  if (!isCompletePermutation(keys, columnKeys(action.column))) return;
  props.actions.edit({
    kind: 'structure',
    commands: [{ op: 'reorderColumn', column: action.column, keys }],
  });
}

function updateMetadata(action: MetadataAction): void {
  if (!sectionMatches(action)) return;
  if (action.field === 'displayName') {
    if (action.value === null) return;
    props.actions.edit({
      kind: 'sectionMetadata',
      sectionKey: action.key,
      change: { field: 'displayName', value: action.value },
    });
    return;
  }
  props.actions.edit({
    kind: 'sectionMetadata',
    sectionKey: action.key,
    change: { field: 'iconKey', value: action.value },
  });
}

function requestDelete(action: SectionAction): void {
  if (!sectionMatches(action)) return;
  const current = currentDocument.value;
  const currentPlacement = placement.value;
  const section = current?.content[action.key];
  if (section === undefined || currentPlacement === undefined) return;
  pendingDelete.value = {
    ...action,
    placement: {
      main: [...currentPlacement.main],
      sidebar: [...currentPlacement.sidebar],
    },
    section: structuredClone(toRaw(section)),
  };
}

function confirmDelete(): void {
  const target = pendingDelete.value;
  if (target === null) return;
  if (!deleteTargetMatches(target)) {
    status.value = 'sectionChanged';
    closeDelete();
    return;
  }
  const result = props.actions.edit({
    kind: 'structure',
    commands: [{ op: 'deleteSection', key: target.key }],
  });
  if (result.kind !== 'enqueued') return;
  closeDelete();
}

function closeDelete(): void {
  pendingDelete.value = null;
}

function deleteTargetMatches(target: DeleteTarget): boolean {
  const current = currentDocument.value;
  const currentPlacement = placement.value;
  const section = current?.content[target.key];
  return (
    section !== undefined
    && currentPlacement !== undefined
    && sectionMatches(target)
    && sameValue(section, target.section)
    && sameValue(currentPlacement, target.placement)
  );
}

function reorderEntries(action: EntryReorderAction): void {
  if (
    !sectionMatches({ key: action.sectionKey, sectionType: action.sectionType })
  ) {
    return;
  }
  const entries
    = currentDocument.value?.content[action.sectionKey]?.entries ?? [];
  const currentIds = entries.map((entry) => entry.id);
  if (!isCompletePermutation(action.entryIds, currentIds)) return;
  props.actions.edit({
    kind: 'entryReorder',
    sectionKey: action.sectionKey,
    entryIds: [...action.entryIds],
  });
}

async function reopen(conflict: ReopenConflict): Promise<void> {
  await props.actions.acceptLatest(conflict.id);
  await nextTick();
  if (
    conflict.command.kind === 'entryReorder'
    && !entryOrderSectionMatches(conflict)
  ) {
    status.value = 'sectionMissing';
    root.value?.$el
      ?.querySelector<HTMLElement>('[data-action="section-type"]')
      ?.focus();
    return;
  }
  const selector
    = conflict.command.kind === 'entryReorder'
      ? `[data-entry-order="${conflict.command.sectionKey}"] button`
      : `[data-section="${structureKey(conflict.command)}"] button`;
  const target = root.value?.$el?.querySelector<HTMLElement>(selector);
  if (target !== null && target !== undefined) {
    target.focus();
    return;
  }
  if (conflict.command.kind === 'structure') {
    status.value = 'sectionMissing';
    root.value?.$el
      ?.querySelector<HTMLElement>('[data-action="section-type"]')
      ?.focus();
  }
}

function entryOrderSectionMatches(conflict: ReopenConflict): boolean {
  if (conflict.command.kind !== 'entryReorder') return false;
  const expected = conflict.command.base.context.sectionType;
  const section = currentDocument.value?.content[conflict.command.sectionKey];
  return (
    expected?.present === true
    && typeof expected.value === 'string'
    && section?.sectionType === expected.value
    && sectionMatches({
      key: conflict.command.sectionKey,
      sectionType: section.sectionType,
    })
  );
}

function isStructureConflict(conflict: unknown): conflict is ReopenConflict {
  return isAtomicConflict(conflict) && conflict.command.kind === 'structure';
}

function isEntryOrderConflict(conflict: unknown): conflict is ReopenConflict {
  return isAtomicConflict(conflict) && conflict.command.kind === 'entryReorder';
}

function isAtomicConflict(conflict: unknown): conflict is AtomicConflictRecord {
  return (
    typeof conflict === 'object'
    && conflict !== null
    && 'subject' in conflict
    && conflict.subject === 'atomic'
  );
}

function focusIssue(issue: ServerValidationIssue): void {
  const match = new RegExp(
    '^content\\.([a-z]+|[0-9a-f-]{36})\\.(displayName|iconKey|entries)$',
  ).exec(issue.path);
  const selector
    = match === null
      ? '[data-section] button'
      : match[2] === 'displayName' || match[2] === 'iconKey'
        ? `[data-section="${match[1]}"] [data-action="${match[2]}"]`
        : `[data-entry-order="${match[1]}"] button`;
  root.value?.$el?.querySelector<HTMLElement>(selector)?.focus();
}

function isStructureIssue(issue: ServerValidationIssue): boolean {
  return (
    issue.path === 'customization.layout.sections'
    || /^content\.([a-z]+|[0-9a-f-]{36})\.(displayName|iconKey|entries)$/.test(
      issue.path,
    )
  );
}

function structureKey(
  command: Extract<AtomicEditorCommand, { kind: 'structure' }>,
): string {
  return (
    command.commands.find((entry) => entry.op !== 'reorderColumn')?.key
    ?? command.commands.find((entry) => entry.op === 'reorderColumn')?.keys[0]
    ?? ''
  );
}

function isRepositoryUuid(key: string): boolean {
  return /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(
    key,
  );
}

function isCompletePermutation(
  candidate: readonly string[],
  current: readonly string[],
): boolean {
  return (
    candidate.length === current.length
    && new Set(candidate).size === candidate.length
    && candidate.every((key) => current.includes(key))
  );
}

function sameValue(left: unknown, right: unknown): boolean {
  if (Object.is(left, right)) return true;
  if (Array.isArray(left) && Array.isArray(right)) {
    return (
      left.length === right.length
      && left.every((value, index) => sameValue(value, right[index]))
    );
  }
  if (!isRecord(left) || !isRecord(right)) return false;
  const leftKeys = Object.keys(left).sort();
  const rightKeys = Object.keys(right).sort();
  return (
    leftKeys.length === rightKeys.length
    && leftKeys.every(
      (key, index) =>
        key === rightKeys[index] && sameValue(left[key], right[key]),
    )
  );
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value);
}
</script>

<template>
  <InspectorPanel
    ref="root"
    :title="copy.sections"
    title-id="structure-title"
  >
    <Card>
      <CardHeader><CardTitle>{{ copy.addSectionTitle }}</CardTitle></CardHeader>
      <CardContent>
        <form
          data-testid="section-create-form"
          @submit.prevent="createSection"
        >
          <SelectField
            v-model="newSectionType"
            :label="copy.sectionType"
            :options="[
              { value: 'profile', label: copy.sectionTypeProfile },
              { value: 'work', label: copy.sectionTypeWork },
              { value: 'education', label: copy.sectionTypeEducation },
              { value: 'skill', label: copy.sectionTypeSkill },
              { value: 'language', label: copy.sectionTypeLanguage },
              { value: 'certificate', label: copy.sectionTypeCertificate },
              { value: 'project', label: copy.sectionTypeProject },
              { value: 'custom', label: copy.sectionTypeCustom },
            ]"
            :control-attrs="{ 'data-action': 'section-type' }"
          />
          <SelectField
            v-model="newColumn"
            :label="copy.column"
            name="column"
            :options="[
              { value: 'main', label: copy.main },
              { value: 'sidebar', label: copy.sidebar },
            ]"
          />
          <FormField
            v-slot="{ id }"
            :label="copy.sectionName"
            name="displayName"
          >
            <Input
              :id="id"
              v-model="newDisplayName"
            />
          </FormField>
          <Button
            type="submit"
            data-action="create"
          >
            {{ copy.addSection }}
          </Button>
        </form>
      </CardContent>
    </Card>
    <StatusBanner
      v-if="statusText !== ''"
      kind="info"
    >
      {{ statusText }}
    </StatusBanner>
    <StatusBanner
      v-if="structureIssues.length > 0"
      kind="error"
    >
      {{ copy.sectionIssue }}
    </StatusBanner>
    <Card
      v-for="item in sections"
      :key="item.key"
      :data-section="item.key"
    >
      <CardContent>
        <SectionControls
          :column="item.column"
          :disabled="sectionDisabled(item.key, item.section)"
          :index="item.index"
          :section="item.section"
          :section-count="columnKeys(item.column).length"
          :section-key="item.key"
          :main-count="columnKeys('main').length"
          :sidebar-count="columnKeys('sidebar').length"
          @delete="requestDelete"
          @metadata="updateMetadata"
          @move="move"
          @reorder="reorder"
        />
        <EntryOrderControls
          :disabled="sectionDisabled(item.key, item.section)"
          :entries="item.section.entries"
          :section-key="item.key"
          :section-type="item.section.sectionType"
          @reorder="reorderEntries"
        />
      </CardContent>
    </Card>
    <ConfirmDialog
      v-if="pendingDelete !== null"
      :open="true"
      :title="copy.deleteSectionTitle"
      :description="copy.deleteSectionDescription(pendingDelete.key)"
      :confirm-label="copy.deleteSection"
      :cancel-label="copy.cancel"
      destructive
      confirm-action="confirm-delete"
      cancel-action="cancel-delete"
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
    <StatusBanner
      v-for="conflict in structureConflicts"
      :key="conflict.id"
      kind="info"
    >
      {{ copy.sectionPlacementChanged }}
      <Button
        type="button"
        data-action="reopen-placement"
        size="sm"
        variant="outline"
        @click="reopen(conflict)"
      >
        {{ copy.reopenPlacement }}
      </Button>
    </StatusBanner>
    <StatusBanner
      v-for="conflict in entryOrderConflicts"
      :key="conflict.id"
      kind="info"
    >
      {{ copy.entryOrderChanged }}
      <Button
        type="button"
        data-action="reopen-entry-order"
        size="sm"
        variant="outline"
        @click="reopen(conflict)"
      >
        {{ copy.reopenOrder }}
      </Button>
    </StatusBanner>
  </InspectorPanel>
</template>
      :cancel-label="copy.cancel"
