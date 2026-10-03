<script setup lang="ts">
import { X } from '@lucide/vue';
import { computed, ref } from 'vue';

import { Button } from '@/components/ui/button';
import type { Locale } from '@/i18n/locale';
import { showcaseCopy } from '@/i18n/showcase';
import {
  activeFilterList,
  type ShowcaseFilterKind,
  type ShowcaseFilters,
} from '@/lib/showcaseQuery';
import { galleryTemplate } from '@/templates/catalog';

// One removable chip per active filter, then Clear all
// (docs/design/ui/showcase.md, Active filters). Removing a chip moves focus
// to the next chip, else the previous one; when none is left the page moves
// focus (Filters button below 1024 px, the rail's pressed Role option from
// 1024 px).
const props = defineProps<{
  readonly filters: ShowcaseFilters;
  readonly locale: Locale;
}>();
const emit = defineEmits<{
  /** `last` is true when no chip is left to take focus. */
  'remove': [kind: ShowcaseFilterKind, last: boolean];
  'clear-all': [];
}>();
const copy = computed(() => showcaseCopy[props.locale]);
const root = ref<HTMLElement | null>(null);
const chips = computed(() => activeFilterList(props.filters).map((filter) => ({
  ...filter,
  label: label(filter.kind, filter.value),
})));

function label(kind: ShowcaseFilterKind, value: string): string {
  if (kind === 'role') {
    return copy.value.roles[value as keyof typeof copy.value.roles];
  }
  if (kind === 'lang') {
    return copy.value.languages[value as keyof typeof copy.value.languages];
  }
  return galleryTemplate(value)?.name ?? copy.value.customDesign;
}

function onRemove(kind: ShowcaseFilterKind): void {
  const buttons = [
    ...root.value?.querySelectorAll<HTMLElement>(
      '[data-action="showcase-filter-remove"]',
    ) ?? [],
  ];
  const index = buttons.findIndex(
    (button) => button.getAttribute('data-filter') === kind,
  );
  const target = buttons[index + 1] ?? buttons[index - 1];
  // The other chips keep their elements, so they can take focus before the
  // removed one leaves the DOM.
  target?.focus();
  emit('remove', kind, target === undefined);
}
</script>

<template>
  <div
    ref="root"
    :aria-label="copy.activeFilters"
    class="flex flex-wrap items-center gap-2"
    data-testid="showcase-active-filters"
    role="group"
  >
    <Button
      v-for="chip in chips"
      :key="chip.kind"
      :aria-label="copy.removeFilter(chip.label)"
      class="showcase-chip"
      data-action="showcase-filter-remove"
      :data-filter="chip.kind"
      type="button"
      variant="outline"
      @click="onRemove(chip.kind)"
    >
      <span class="showcase-chip__label">{{ chip.label }}</span>
      <X
        aria-hidden="true"
        class="size-3.5"
      />
    </Button>
    <Button
      class="showcase-clear-all"
      data-action="showcase-filters-clear-all"
      type="button"
      variant="ghost"
      @click="emit('clear-all')"
    >
      {{ copy.clearAll }}
    </Button>
  </div>
</template>

<style scoped>
.showcase-chip {
  gap: 4px;
  height: 36px;
  padding: 0 10px 0 12px;
  border: 1px solid var(--border);
  border-radius: 9999px;
  background: var(--secondary);
  color: var(--foreground);
  font-size: 0.875rem;
  font-weight: 500;
}

.showcase-chip:hover {
  background: var(--accent);
}

.showcase-chip__label {
  max-width: 14rem;
  overflow: hidden;
  text-overflow: ellipsis;
}

.showcase-clear-all {
  height: 36px;
  padding: 0 8px;
  background: transparent;
  color: var(--link);
  font-size: 0.875rem;
  font-weight: 500;
  text-decoration: underline;
}

.showcase-clear-all:hover {
  background: transparent;
  color: var(--link);
  text-decoration: underline;
}
</style>
