<script setup lang="ts">
import { X } from '@lucide/vue';
import { ref } from 'vue';

import { Button } from '@/components/ui/button';
import type { FilterControlCopy } from '@/i18n/filters';

const props = defineProps<{
  readonly prefix: 'showcase' | 'gallery';
  readonly chips: readonly { kind: string; label: string }[];
  readonly copy: FilterControlCopy;
  readonly disabled?: boolean;
}>();
const emit = defineEmits<{
  'remove': [kind: string, last: boolean];
  'clear-all': [];
}>();
const root = ref<HTMLElement | null>(null);

function onRemove(kind: string): void {
  const buttons = [
    ...(root.value?.querySelectorAll<HTMLElement>(
      `[data-action="${props.prefix}-filter-remove"]`,
    ) ?? []),
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
    :data-testid="`${prefix}-active-filters`"
    role="group"
  >
    <Button
      v-for="chip in chips"
      :key="chip.kind"
      :aria-label="copy.removeFilter(chip.label)"
      class="showcase-chip"
      :data-action="`${prefix}-filter-remove`"
      :data-filter="chip.kind"
      :disabled="disabled"
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
      :data-action="`${prefix}-filters-clear-all`"
      :disabled="disabled"
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
