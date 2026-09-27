<script setup lang="ts">
/**
 * One entry row in the review's section cards: a checkbox, the entry's text
 * (label and description, plain text only), and an optional notice or
 * invalid mark (docs/design/linkedin-import-ui.md, "Review state", the entry
 * table).
 */
import { CircleAlert, Info } from '@lucide/vue';
import { computed, useId } from 'vue';
import { Checkbox } from '@/components/ui/checkbox';
import { Label } from '@/components/ui/label';
import type { ImportCopy } from '@/i18n/import';
import type { ReviewEntry } from '@/import/linkedin/build';
import {
  entryDescription,
  entryIndicator,
  entryLabel,
  type DateFormat,
  richTextToPlainText,
} from '@/import/linkedin/pageState';
import type { Locale } from '@/i18n/locale';

const props = defineProps<{
  readonly copy: ImportCopy;
  readonly entry: ReviewEntry;
  readonly locale: Locale;
  readonly dateFormat: DateFormat;
  readonly invalidEntryIds: ReadonlySet<string>;
  readonly modelValue: boolean;
  readonly disabled: boolean;
}>();
const emit = defineEmits<{ 'update:modelValue': [value: boolean] }>();

const generated = useId();
const fieldId = computed(() => `import-entry-${generated}`);
const descriptionId = computed(() => `${fieldId.value}-description`);
const noticeId = computed(() => `${fieldId.value}-notice`);

const label = computed(() => (
  props.entry.section === 'profile'
    ? props.copy.summaryEntry
    : entryLabel(props.entry.section, props.entry.entry)
));
const description = computed(() => {
  if (props.entry.section === 'profile') {
    const html = props.entry.entry.text;
    return typeof html === 'string' ? richTextToPlainText(html) : '';
  }
  return entryDescription(
    props.entry.section, props.entry.entry, props.locale, props.dateFormat,
  );
});
const indicator = computed(() =>
  entryIndicator(props.entry, props.invalidEntryIds));
const describedBy = computed(() => {
  const ids = [
    description.value !== undefined ? descriptionId.value : undefined,
    indicator.value !== undefined ? noticeId.value : undefined,
  ].filter((id): id is string => id !== undefined);
  return ids.length > 0 ? ids.join(' ') : undefined;
});
</script>

<template>
  <li class="flex min-h-11 items-start gap-2 border-t py-3 first:border-t-0">
    <Checkbox
      :id="fieldId"
      :data-import-entry="entry.id"
      :aria-describedby="describedBy"
      class="mt-0.5"
      :disabled="disabled"
      :model-value="modelValue"
      @update:model-value="(value) => emit('update:modelValue', Boolean(value))"
    />
    <!-- The label wraps the whole text column, not just the heading, so the
      whole row is the hit area (docs/design/linkedin-import-ui.md, "Review
      state"). Its lines are spans, phrasing content, since a label cannot
      hold block-level children. -->
    <Label
      class="grid flex-1 cursor-pointer items-start gap-1 font-normal"
      :for="fieldId"
    >
      <span class="text-sm font-medium">{{ label }}</span>
      <span
        v-if="description !== undefined"
        :id="descriptionId"
        class="whitespace-pre-line text-sm text-muted-foreground"
        :class="entry.section === 'profile' && 'line-clamp-3'"
      >
        {{ description }}
      </span>
      <span
        v-if="indicator === 'invalid'"
        :id="noticeId"
        class="flex items-center gap-1 text-xs text-destructive"
      >
        <CircleAlert
          aria-hidden="true"
          class="size-3.5"
        />
        <span>{{ copy.entryInvalid }}</span>
      </span>
      <span
        v-else-if="indicator !== undefined"
        :id="noticeId"
        class="flex items-center gap-1 text-xs text-foreground"
      >
        <Info
          aria-hidden="true"
          class="size-3.5 text-brand-indigo"
        />
        <span>{{
          indicator === 'noDates' ? copy.entryNoDates : copy.entryCut
        }}</span>
      </span>
    </Label>
  </li>
</template>
