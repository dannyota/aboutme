<script setup lang="ts">
import { computed } from 'vue';

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

// The role row, the language row, and the template select. A change emits
// the whole next filter set; the page writes it to the URL
// (docs/design/ui/landing-and-library.md, Community showcase, Filters).
const props = defineProps<{
  readonly filters: ShowcaseFilters;
  readonly locale: Locale;
}>();
const emit = defineEmits<{ change: [filters: ShowcaseFilters] }>();
const copy = computed(() => showcaseCopy[props.locale]);

const templateOptions = computed(() => [
  { value: '', label: copy.value.allTemplates },
  ...GALLERY
    .map((template) => ({ value: template.id, label: template.name }))
    .sort((left, right) => left.label.localeCompare(right.label, 'en')),
  { value: SHOWCASE_CUSTOM_TEMPLATE, label: copy.value.customDesign },
]);

// Reka's single toggle group emits undefined when the pressed item is
// pressed again; a chip stays selected, like a radio.
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
  <div data-testid="showcase-filters">
    <div
      class="showcase-scroll -mx-4 mt-8 overflow-x-auto px-4 sm:mx-0 sm:px-0"
    >
      <ToggleGroup
        :aria-label="copy.rolesLabel"
        class="flex w-max items-center gap-2"
        data-testid="showcase-roles"
        :model-value="filters.role ?? 'all'"
        :spacing="2"
        type="single"
        @update:model-value="onRole"
      >
        <ToggleGroupItem
          class="showcase-chip"
          data-role="all"
          value="all"
        >
          {{ copy.allRoles }}
        </ToggleGroupItem>
        <ToggleGroupItem
          v-for="role in SHOWCASE_ROLES"
          :key="role"
          class="showcase-chip"
          :data-role="role"
          :value="role"
        >
          {{ copy.roles[role] }}
        </ToggleGroupItem>
      </ToggleGroup>
    </div>
    <div class="mt-3 flex flex-wrap items-center gap-x-3 gap-y-3">
      <div class="showcase-scroll max-w-full overflow-x-auto">
        <ToggleGroup
          :aria-label="copy.languageLabel"
          class="flex w-max items-center gap-2"
          data-testid="showcase-languages"
          :model-value="filters.lang ?? 'all'"
          :spacing="2"
          type="single"
          @update:model-value="onLanguage"
        >
          <ToggleGroupItem
            class="showcase-chip"
            data-lang="all"
            value="all"
          >
            {{ copy.allLanguages }}
          </ToggleGroupItem>
          <ToggleGroupItem
            v-for="lang in SHOWCASE_FILTER_LANGUAGES"
            :key="lang"
            class="showcase-chip"
            :data-lang="lang"
            :value="lang"
          >
            {{ copy.languages[lang] }}
          </ToggleGroupItem>
        </ToggleGroup>
      </div>
      <SelectField
        class="showcase-template flex items-center gap-2 max-[640px]:w-full"
        :label="copy.templateLabel"
        :model-value="filters.template ?? ''"
        name="template"
        :options="templateOptions"
        @update:model-value="onTemplate"
      />
    </div>
  </div>
</template>

<style scoped>
.showcase-scroll {
  scrollbar-width: none;
}

.showcase-scroll::-webkit-scrollbar {
  display: none;
}

.showcase-chip {
  display: inline-flex;
  align-items: center;
  height: 2.25rem;
  padding: 0 1rem;
  border: 1px solid var(--border);
  border-radius: 9999px;
  background: var(--card);
  color: var(--foreground);
  font-size: 0.875rem;
  font-weight: 500;
  white-space: nowrap;
  transition: background-color 150ms, border-color 150ms;
}

.showcase-chip:not([aria-pressed="true"]):not([data-state="on"]):hover {
  background: var(--surface-indigo);
}

.showcase-chip[aria-pressed="true"],
.showcase-chip[data-state="on"] {
  border-color: var(--primary);
  background: var(--primary);
  color: var(--primary-foreground);
}

.showcase-chip:focus-visible {
  outline: 2px solid var(--ring);
  outline-offset: 2px;
}

.showcase-template :deep([data-slot="native-select-wrapper"]) {
  min-width: 12rem;
  max-width: 16rem;
}

@media (width <= 640px) {
  .showcase-template :deep([data-slot="native-select-wrapper"]) {
    flex: 1;
    max-width: none;
  }
}
</style>
