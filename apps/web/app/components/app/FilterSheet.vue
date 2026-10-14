<script setup lang="ts">
import { X } from '@lucide/vue';

import { Button } from '@/components/ui/button';
import { SheetClose, SheetContent, SheetTitle } from '@/components/ui/sheet';
import type { FilterControlCopy } from '@/i18n/filters';

defineProps<{
  readonly prefix: 'showcase' | 'gallery';
  readonly copy: FilterControlCopy;
  readonly resultsLabel: string;
  readonly status: string;
  readonly announceKey: number;
}>();
const emit = defineEmits<{
  'clear': [];
  'close-auto-focus': [event: Event];
}>();
</script>

<template>
  <SheetContent
    :aria-describedby="undefined"
    class="max-h-[85vh] gap-0 rounded-t-[var(--radius-feature)] border-border
      bg-popover supports-[height:1dvh]:max-h-[85dvh]
      min-[641px]:mx-auto min-[641px]:max-w-[40rem]"
    :data-testid="`${prefix}-filter-sheet`"
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
          :data-action="`${prefix}-filters-close`"
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
      <slot />
    </div>
    <div
      class="grid shrink-0 grid-cols-2 gap-3 border-t border-border px-4 pt-3
        pb-[calc(16px+env(safe-area-inset-bottom))]"
    >
      <Button
        class="h-12 rounded-[10px] px-3 text-[15px] font-medium"
        :data-action="`${prefix}-filters-clear`"
        type="button"
        variant="outline"
        @click="emit('clear')"
      >
        {{ copy.clearFilters }}
      </Button>
      <SheetClose as-child>
        <Button
          class="h-12 rounded-[10px] px-3 text-[15px] font-semibold"
          :data-action="`${prefix}-filters-apply`"
          type="button"
        >
          {{ resultsLabel }}
        </Button>
      </SheetClose>
    </div>
    <p
      aria-atomic="true"
      class="sr-only"
      :data-testid="`${prefix}-sheet-status`"
      role="status"
    >
      <span :key="announceKey">{{ status }}</span>
    </p>
  </SheetContent>
</template>
