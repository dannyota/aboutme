<script setup lang="ts">
/**
 * The review form: notices, the resume and personal detail cards, one card
 * per found section, the check line, and the not-imported card
 * (docs/design/linkedin-import-ui.md, "Review state").
 */
import { CircleAlert } from '@lucide/vue';
import { computed } from 'vue';
import CheckboxField from '@/components/app/CheckboxField.vue';
import StatusBanner from '@/components/app/StatusBanner.vue';
import TextField from '@/components/app/TextField.vue';
import { Checkbox } from '@/components/ui/checkbox';
import { editorFieldsCopy } from '@/i18n/editor-fields';
import type { ImportCopy } from '@/i18n/import';
import type { ResumeCreateCopy } from '@/i18n/resume-create';
import type { Locale } from '@/i18n/locale';
import type { ImportReview, SchemaCheck } from '@/import/linkedin/build';
import {
  buildNoticeLines,
  groupCheckState,
  importSectionName,
  sectionCount,
  type DateFormat,
} from '@/import/linkedin/pageState';
import ImportEntryRow from './ImportEntryRow.vue';

const props = defineProps<{
  readonly copy: ImportCopy;
  readonly createCopy: ResumeCreateCopy;
  readonly locale: Locale;
  readonly review: ImportReview;
  readonly dateFormat: DateFormat;
  readonly title: string;
  readonly fullName: string;
  readonly headline: string;
  readonly detailIds: ReadonlySet<string>;
  readonly entryIds: ReadonlySet<string>;
  readonly schemaCheck: SchemaCheck;
  readonly disabled: boolean;
  readonly titleInvalid: boolean;
}>();
const emit = defineEmits<{
  'update:title': [value: string];
  'update:fullName': [value: string];
  'update:headline': [value: string];
  'update:detailIds': [value: Set<string>];
  'update:entryIds': [value: Set<string>];
}>();

const notices = computed(() => buildNoticeLines(props.locale, props.review));
// checkDocument maps a failing path to the entry or detail id it belongs to,
// so this set covers both (apps/web/app/import/linkedin/build.ts,
// `checkDocument`).
const invalidEntryIds = computed((): ReadonlySet<string> =>
  props.schemaCheck.ok ? new Set() : new Set(props.schemaCheck.entryIds));
const personal = computed(() => editorFieldsCopy[props.locale].personal);

function noticeText(line: ReturnType<typeof buildNoticeLines>[number]): string {
  const copy = props.copy;
  switch (line.kind) {
    case 'dates': return copy.noticeDates(line.entry);
    case 'startOnly': return copy.noticeStartOnly(line.entry);
    case 'cut': return copy.noticeCut(line.field, line.max);
    case 'overLimit': return copy.noticeOverLimit(line.section);
  }
}

type DetailType = 'email' | 'phone' | 'location' | 'linkedin' | 'website';

function detailFieldName(type: DetailType): string {
  return personal.value[type];
}
/** Email and phone start unchecked, because a published resume shows them;
 * `contactOffHint` explains that on its own line under the value (docs/
 * design/linkedin-import-ui.md, "Personal details card"). */
function isContactField(type: DetailType): boolean {
  return type === 'email' || type === 'phone';
}
function detailHintId(id: string): string {
  return `import-detail-${id}-hint`;
}
function detailInvalidId(id: string): string {
  return `import-detail-${id}-invalid`;
}
function detailDescribedBy(detail: { id: string; type: DetailType }):
string | undefined {
  const ids = [
    isContactField(detail.type) ? detailHintId(detail.id) : undefined,
    invalidEntryIds.value.has(detail.id)
      ? detailInvalidId(detail.id)
      : undefined,
  ].filter((id): id is string => id !== undefined);
  return ids.length > 0 ? ids.join(' ') : undefined;
}
function toggleDetail(id: string, checked: boolean): void {
  const next = new Set(props.detailIds);
  if (checked) next.add(id);
  else next.delete(id);
  emit('update:detailIds', next);
}
function toggleEntry(id: string, checked: boolean): void {
  const next = new Set(props.entryIds);
  if (checked) next.add(id);
  else next.delete(id);
  emit('update:entryIds', next);
}
function toggleGroupChecked(
  entries: readonly { id: string }[],
  checked: boolean,
): void {
  const next = new Set(props.entryIds);
  for (const entry of entries) {
    if (checked) next.add(entry.id);
    else next.delete(entry.id);
  }
  emit('update:entryIds', next);
}
type OneSection = ImportReview['sections'][number];

function groupModel(section: OneSection): boolean | 'indeterminate' {
  const state = groupCheckState(section, props.entryIds);
  return state === 'indeterminate' ? 'indeterminate' : state === 'checked';
}
</script>

<template>
  <div class="grid gap-6">
    <StatusBanner
      v-if="notices.length > 0"
      data-import-notices
      kind="info"
      :title="copy.noticesHeading"
    >
      <ul class="grid gap-1">
        <li
          v-for="(line, index) in notices"
          :key="index"
        >
          {{ noticeText(line) }}
        </li>
      </ul>
    </StatusBanner>

    <section
      aria-labelledby="import-resume-heading"
      class="grid gap-3 rounded-lg border bg-card p-4 shadow-product sm:p-6"
    >
      <h2
        id="import-resume-heading"
        class="text-base font-semibold"
      >
        {{ copy.resumeHeading }}
      </h2>
      <TextField
        :control-attrs="{ 'data-action': 'import-title' }"
        :disabled="disabled"
        :error="titleInvalid ? createCopy.titleRequired : undefined"
        :label="createCopy.title"
        :model-value="title"
        name="title"
        required
        @update:model-value="emit('update:title', $event)"
      />
      <p class="text-sm text-muted-foreground">
        {{ copy.languageLine }}
      </p>
      <p class="text-sm text-muted-foreground">
        {{ copy.templateLine(review.template.name) }}
      </p>
    </section>

    <section
      aria-labelledby="import-personal-heading"
      class="grid gap-3 rounded-lg border bg-card p-4 shadow-product sm:p-6"
    >
      <h2
        id="import-personal-heading"
        class="text-base font-semibold"
      >
        {{ personal.title }}
      </h2>
      <TextField
        :disabled="disabled"
        :label="personal.fullName"
        :model-value="fullName"
        name="fullName"
        @update:model-value="emit('update:fullName', $event)"
      />
      <TextField
        :disabled="disabled"
        :label="personal.headline"
        :model-value="headline"
        name="headline"
        @update:model-value="emit('update:headline', $event)"
      />
      <ul
        v-if="review.details.length > 0"
        class="grid gap-1"
      >
        <li
          v-for="detail in review.details"
          :key="detail.id"
        >
          <CheckboxField
            :aria-describedby="detailDescribedBy(detail)"
            :data-import-entry="detail.id"
            :description="detailFieldName(detail.type)"
            :disabled="disabled"
            :label="detail.value"
            :model-value="detailIds.has(detail.id)"
            @update:model-value="(value) => toggleDetail(detail.id, value)"
          />
          <p
            v-if="isContactField(detail.type)"
            :id="detailHintId(detail.id)"
            class="pl-6 text-sm text-muted-foreground"
          >
            {{ copy.contactOffHint }}
          </p>
          <p
            v-if="invalidEntryIds.has(detail.id)"
            :id="detailInvalidId(detail.id)"
            class="flex items-center gap-1 pl-6 text-xs text-destructive"
          >
            <CircleAlert
              aria-hidden="true"
              class="size-3.5"
            />
            <span>{{ copy.entryInvalid }}</span>
          </p>
        </li>
      </ul>
    </section>

    <section
      v-for="section in review.sections"
      :key="section.key"
      :aria-labelledby="`import-section-${section.key}-heading`"
      class="grid gap-3 rounded-lg border bg-card p-4 shadow-product sm:p-6"
    >
      <div class="flex items-center gap-2">
        <Checkbox
          :aria-label="copy.groupLabel(importSectionName(locale, section.key))"
          :disabled="disabled"
          :model-value="groupModel(section)"
          @update:model-value="(value) =>
            toggleGroupChecked(section.entries, value !== false)"
        />
        <h2
          :id="`import-section-${section.key}-heading`"
          class="text-base font-semibold"
        >
          {{ importSectionName(locale, section.key) }}
        </h2>
        <span class="ml-auto text-sm text-muted-foreground">
          {{ copy.groupCount(
            sectionCount(section, entryIds).n,
            sectionCount(section, entryIds).m,
          ) }}
        </span>
      </div>
      <ul>
        <ImportEntryRow
          v-for="entry in section.entries"
          :key="entry.id"
          :copy="copy"
          :date-format="dateFormat"
          :disabled="disabled"
          :entry="entry"
          :invalid-entry-ids="invalidEntryIds"
          :locale="locale"
          :model-value="entryIds.has(entry.id)"
          @update:model-value="(value) => toggleEntry(entry.id, value)"
        />
      </ul>
    </section>

    <p class="text-sm text-muted-foreground">
      {{ copy.checkLine }}
    </p>

    <section
      v-if="review.dropped.length > 0"
      aria-labelledby="import-not-imported-heading"
      class="grid gap-2 rounded-lg border bg-card p-4 shadow-product sm:p-6"
    >
      <h2
        id="import-not-imported-heading"
        class="text-base font-semibold"
      >
        {{ copy.notImportedHeading }}
      </h2>
      <ul class="grid gap-1 text-sm text-muted-foreground">
        <li
          v-for="dropped in review.dropped"
          :key="dropped.heading"
        >
          {{ copy.notImportedLine(dropped.heading, dropped.count) }}
        </li>
      </ul>
      <p class="text-sm text-muted-foreground">
        {{ copy.notImportedNote }}
      </p>
    </section>
  </div>
</template>
