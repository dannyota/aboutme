<script setup lang="ts">
import { X } from '@lucide/vue';
import { computed } from 'vue';

import ShowcaseFilters from '@/components/showcase/ShowcaseFilters.vue';
import { Button } from '@/components/ui/button';
import {
  SheetClose,
  SheetContent,
  SheetTitle,
} from '@/components/ui/sheet';
import type { Locale } from '@/i18n/locale';
import { showcaseCopy } from '@/i18n/showcase';
import type { ShowcaseFilters as Filters } from '@/lib/showcaseQuery';

// The bottom sheet below 1024 px: the shared filter fields, Clear filters,
// and Show results (docs/design/ui/showcase.md, Bottom sheet). Its content
// mounts only while it is open. Every change applies at once through
// `change`; the page replaces the URL query and reloads behind the sheet.
const props = defineProps<{
  readonly filters: Filters;
  readonly locale: Locale;
  /** The live listing total; null while a new total loads. */
  readonly total: number | null;
  /** The count line text, mirrored in the sheet's own status region. */
  readonly status: string;
}>();
const emit = defineEmits<{
  'change': [filters: Filters];
  'close-auto-focus': [event: Event];
}>();
const copy = computed(() => showcaseCopy[props.locale]);
const applyLabel = computed(() => (
  props.total === null
    ? copy.value.showResultsLoading
    : copy.value.showResults(props.total)
));
</script>

<template>
  <SheetContent
    :aria-describedby="undefined"
    class="max-h-[85vh] gap-0 rounded-t-[var(--radius-feature)] border-border
      bg-popover supports-[height:1dvh]:max-h-[85dvh]
      min-[641px]:mx-auto min-[641px]:max-w-[40rem]"
    data-testid="showcase-filter-sheet"
    side="bottom"
    :show-close="false"
    @close-auto-focus="emit('close-auto-focus', $event)"
  >
    <span
      aria-hidden="true"
      class="mx-auto mt-2 block h-1 w-10 shrink-0 rounded-full bg-input"
    />
    <div class="mt-3 flex shrink-0 items-center justify-between px-4">
      <SheetTitle class="text-lg font-bold">
        {{ copy.filters }}
      </SheetTitle>
      <SheetClose as-child>
        <Button
          :aria-label="copy.close"
          class="size-11 rounded-[10px]"
          data-action="showcase-filters-close"
          type="button"
          variant="ghost"
        >
          <X
            aria-hidden="true"
            class="size-5"
          />
        </Button>
      </SheetClose>
    </div>
    <div class="min-h-0 flex-1 overflow-y-auto px-4 py-5">
      <ShowcaseFilters
        :filters="filters"
        layout="sheet"
        :locale="locale"
        @change="emit('change', $event)"
      />
    </div>
    <div
      class="grid shrink-0 grid-cols-2 gap-3 border-t border-border px-4 pt-3
        pb-[calc(16px+env(safe-area-inset-bottom))]"
    >
      <Button
        class="h-12 rounded-[10px] px-3 text-[15px] font-medium"
        data-action="showcase-filters-clear"
        type="button"
        variant="outline"
        @click="emit('change', {})"
      >
        {{ copy.clearFilters }}
      </Button>
      <SheetClose as-child>
        <Button
          class="h-12 rounded-[10px] px-3 text-[15px] font-semibold"
          data-action="showcase-filters-apply"
          type="button"
        >
          {{ applyLabel }}
        </Button>
      </SheetClose>
    </div>
    <p
      aria-atomic="true"
      class="sr-only"
      data-testid="showcase-sheet-status"
      role="status"
    >
      {{ status }}
    </p>
  </SheetContent>
</template>
