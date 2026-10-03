<script setup lang="ts">
import { computed, useId } from 'vue';

import SelectField from '@/components/app/SelectField.vue';
import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group';
import type { Locale } from '@/i18n/locale';
import { showcaseCopy } from '@/i18n/showcase';
import {
  SHOWCASE_CUSTOM_TEMPLATE,
  SHOWCASE_FILTER_LANGUAGES,
  SHOWCASE_ROLES,
} from '@/lib/showcaseContract';
import type { ShowcaseFilters } from '@/lib/showcaseQuery';
import { GALLERY } from '@/templates/catalog';

// The role group, the language group, and the template select, shared by the
// bottom sheet and the left rail (docs/design/ui/showcase.md, Filters below
// 1024 px and Rail from 1024 px). A change emits the whole next filter set;
// the page writes it to the URL. Element ids come from useId(), so the rail
// and an open sheet never share one.
const props = defineProps<{
  readonly filters: ShowcaseFilters;
  readonly locale: Locale;
  readonly layout: 'sheet' | 'rail';
}>();
const emit = defineEmits<{ change: [filters: ShowcaseFilters] }>();
const copy = computed(() => showcaseCopy[props.locale]);
const uid = useId();
const languageHeadingId = `showcase-language-heading-${uid}`;
const templateId = `showcase-template-${uid}`;
const orientation = computed(() =>
  props.layout === 'rail' ? 'vertical' : undefined);

const templateOptions = computed(() => [
  { value: '', label: copy.value.allTemplates },
  ...GALLERY
    .map((template) => ({ value: template.id, label: template.name }))
    .sort((left, right) => left.label.localeCompare(right.label, 'en')),
  { value: SHOWCASE_CUSTOM_TEMPLATE, label: copy.value.customDesign },
]);

// Reka's single toggle group emits undefined when the pressed item is
// pressed again; an option stays selected, like a radio.
function onRole(value: unknown): void {
  if (value === undefined) return;
  const role = SHOWCASE_ROLES.find((known) => known === value);
  if (value !== 'all' && role === undefined) return;
  emit('change', { ...props.filters, role });
}

function onLanguage(value: unknown): void {
  if (value === undefined) return;
  const lang = SHOWCASE_FILTER_LANGUAGES.find((known) => known === value);
  if (value !== 'all' && lang === undefined) return;
  emit('change', { ...props.filters, lang });
}

function onTemplate(value: string): void {
  emit('change', {
    ...props.filters,
    template: value === '' ? undefined : value,
  });
}
</script>

<template>
  <div
    class="showcase-filters"
    :class="`showcase-filters--${layout}`"
    data-testid="showcase-filters"
  >
    <div class="showcase-filters__group">
      <p
        class="showcase-filters__heading"
      >
        {{ copy.roleHeading }}
      </p>
      <ToggleGroup
        :aria-label="copy.rolesLabel"
        class="showcase-options showcase-options--roles"
        data-testid="showcase-roles"
        :model-value="filters.role ?? 'all'"
        :orientation="orientation"
        :spacing="2"
        type="single"
        @update:model-value="onRole"
      >
        <ToggleGroupItem
          class="showcase-option"
          data-role="all"
          value="all"
        >
          {{ copy.allRoles }}
        </ToggleGroupItem>
        <ToggleGroupItem
          v-for="role in SHOWCASE_ROLES"
          :key="role"
          class="showcase-option"
          :data-role="role"
          :value="role"
        >
          {{ copy.roles[role] }}
        </ToggleGroupItem>
      </ToggleGroup>
    </div>
    <div class="showcase-filters__group">
      <p
        :id="languageHeadingId"
        class="showcase-filters__heading"
      >
        {{ copy.languageLabel }}
      </p>
      <ToggleGroup
        :aria-labelledby="languageHeadingId"
        class="showcase-options showcase-options--languages"
        data-testid="showcase-languages"
        :model-value="filters.lang ?? 'all'"
        :orientation="orientation"
        :spacing="2"
        type="single"
        @update:model-value="onLanguage"
      >
        <ToggleGroupItem
          class="showcase-option"
          data-lang="all"
          value="all"
        >
          {{ copy.allLanguages }}
        </ToggleGroupItem>
        <ToggleGroupItem
          v-for="lang in SHOWCASE_FILTER_LANGUAGES"
          :key="lang"
          class="showcase-option"
          :data-lang="lang"
          :value="lang"
        >
          {{ copy.languages[lang] }}
        </ToggleGroupItem>
      </ToggleGroup>
    </div>
    <SelectField
      :id="templateId"
      class="showcase-template"
      :control-attrs="{ name: 'template' }"
      :label="copy.templateLabel"
      :model-value="filters.template ?? ''"
      name="template"
      :options="templateOptions"
      @update:model-value="onTemplate"
    />
  </div>
</template>

<style scoped>
.showcase-filters {
  display: grid;
  gap: 20px;
}

.showcase-filters--rail {
  gap: 24px;
}

.showcase-filters__group {
  display: grid;
  gap: 8px;
}

.showcase-filters__heading,
.showcase-template :deep(label) {
  color: var(--muted-foreground);
  font-size: 0.875rem;
  font-weight: 600;
  line-height: 1.5;
}

.showcase-options {
  display: flex;
  flex-wrap: wrap;
  align-items: stretch;
  gap: 8px;
  width: 100%;
}

.showcase-filters :deep(.showcase-option) {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  border: 1px solid var(--border);
  background: var(--card);
  color: var(--foreground);
  font-size: 0.875rem;
  font-weight: 500;
  white-space: nowrap;
  transition: background-color 150ms, border-color 150ms;
}

.showcase-filters :deep(.showcase-option:not([data-state="on"]):hover) {
  background: var(--surface-indigo);
}

.showcase-filters :deep(.showcase-option[aria-pressed="true"]),
.showcase-filters :deep(.showcase-option[data-state="on"]) {
  border-color: var(--primary);
  background: var(--primary);
  color: var(--primary-foreground);
  font-weight: 600;
}

.showcase-filters :deep(.showcase-option:focus-visible) {
  outline: 2px solid var(--ring);
  outline-offset: 2px;
}

/* The options render inside reka's ToggleGroupItem, which does not carry
   this component's scope id when the sheet mounts in the browser, so every
   option rule reaches it through :deep(). */

/* Sheet: role pills wrap, language options share three columns. */
.showcase-filters--sheet .showcase-options--roles :deep(.showcase-option) {
  height: 40px;
  padding: 0 14px;
  border-radius: 9999px;
}

.showcase-filters--sheet .showcase-options--languages {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
}

.showcase-filters--sheet .showcase-options--languages :deep(.showcase-option) {
  min-height: 44px;
  padding: 0 6px;
  border-radius: 10px;
  line-height: 1.2;
  text-align: center;
  white-space: normal;
}

/* Rail: both groups are lists of 36 px rows. */
.showcase-filters--rail .showcase-options {
  flex-direction: column;
  flex-wrap: nowrap;
  gap: 2px;
}

.showcase-filters--rail :deep(.showcase-option) {
  justify-content: flex-start;
  height: 36px;
  padding: 0 12px;
  border-color: transparent;
  border-radius: 10px;
  background: transparent;
  font-weight: 400;
  text-align: left;
}

.showcase-filters--rail :deep(.showcase-option:not([data-state="on"]):hover) {
  background: var(--muted);
}

.showcase-filters--rail :deep(.showcase-option[aria-pressed="true"]),
.showcase-filters--rail :deep(.showcase-option[data-state="on"]) {
  border-color: transparent;
  background: var(--accent);
  color: var(--foreground);
  font-weight: 600;
}

.showcase-filters--rail :deep(.showcase-option[aria-pressed="true"]::after),
.showcase-filters--rail :deep(.showcase-option[data-state="on"]::after) {
  content: "";
  width: 8px;
  height: 8px;
  margin-left: auto;
  border-radius: 9999px;
  background: var(--primary);
}

.showcase-template {
  gap: 8px;
}

.showcase-template :deep([data-slot="native-select-wrapper"]) {
  width: 100%;
}

.showcase-filters--sheet .showcase-template :deep(select) {
  height: 44px;
}

.showcase-filters--rail .showcase-template :deep(select) {
  height: 40px;
}
</style>
