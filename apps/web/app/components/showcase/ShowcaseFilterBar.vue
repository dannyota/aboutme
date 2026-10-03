<script setup lang="ts">
import { ListFilter } from '@lucide/vue';
import { computed, ref } from 'vue';

import { Button } from '@/components/ui/button';
import { Sheet, SheetTrigger } from '@/components/ui/sheet';
import type { Locale } from '@/i18n/locale';
import { showcaseCopy } from '@/i18n/showcase';

// The count line and the Filters button below 1024 px, where the bar sticks
// under the viewport top; from 1024 px the bar is `display: contents`, the
// count line moves to the header row, and the button is hidden
// (docs/design/ui/showcase.md, Count line and Filters below 1024 px). The
// button is the trigger of the sheet its default slot holds, so focus returns
// to it when the sheet closes.
const props = defineProps<{
  readonly locale: Locale;
  readonly open: boolean;
  /** The count line text; empty before the first load (a skeleton). */
  readonly text: string;
  /** The leading part of `text` shown bold, such as "1,234 resumes". */
  readonly head: string;
  readonly activeCount: number;
}>();
const emit = defineEmits<{ 'update:open': [open: boolean] }>();
const copy = computed(() => showcaseCopy[props.locale]);
const bar = ref<HTMLElement | null>(null);

defineExpose({
  focusFilters: (): void => {
    bar.value
      ?.querySelector<HTMLElement>('[data-action="showcase-filters-open"]')
      ?.focus();
  },
});
</script>

<template>
  <Sheet
    :open="open"
    @update:open="emit('update:open', $event)"
  >
    <div
      ref="bar"
      class="showcase-filter-bar"
      data-testid="showcase-filter-bar"
    >
      <p
        aria-atomic="true"
        class="showcase-count"
        data-testid="showcase-count"
        role="status"
      >
        <span
          v-if="text === ''"
          class="inline-block h-5 w-24 animate-pulse rounded-md bg-primary/10
            align-top"
          data-slot="skeleton"
        />
        <template v-else>
          <strong
            v-if="head !== ''"
            class="showcase-count__head"
          >{{ head }}</strong>{{ text.slice(head.length) }}
        </template>
      </p>
      <SheetTrigger as-child>
        <Button
          :aria-label="activeCount > 0
            ? copy.filtersActive(activeCount)
            : undefined"
          class="showcase-filters-button"
          :class="{ 'is-active': activeCount > 0 }"
          data-action="showcase-filters-open"
          type="button"
          variant="outline"
        >
          <ListFilter
            aria-hidden="true"
            class="size-[18px]"
          />
          {{ copy.filters }}
          <span
            v-if="activeCount > 0"
            aria-hidden="true"
            class="showcase-filters-badge"
            data-testid="showcase-filters-badge"
          >{{ activeCount }}</span>
        </Button>
      </SheetTrigger>
    </div>
    <slot />
  </Sheet>
</template>

<style scoped>
.showcase-filter-bar {
  position: sticky;
  top: 0;
  z-index: 10;
  box-sizing: border-box;
  display: flex;
  align-items: center;
  gap: 8px;
  height: 60px;
  margin: 8px -16px 0;
  padding: 0 16px;
  border-bottom: 1px solid var(--border);
  background: var(--background);
}

.showcase-count {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  color: var(--muted-foreground);
  font-size: 0.875rem;
  line-height: 20px;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.showcase-count__head {
  color: var(--foreground);
  font-weight: 600;
}

.showcase-filters-button {
  height: 44px;
  gap: 8px;
  padding: 0 14px;
  border: 1px solid var(--input);
  border-radius: 10px;
  background: transparent;
  color: var(--foreground);
  font-size: 0.875rem;
  font-weight: 500;
}

.showcase-filters-button:hover {
  background: var(--accent);
}

.showcase-filters-button.is-active {
  padding-right: 10px;
  border-color: var(--primary);
}

.showcase-filters-badge {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  box-sizing: border-box;
  min-width: 22px;
  height: 22px;
  padding: 0 6px;
  border-radius: 9999px;
  background: var(--primary);
  color: var(--primary-foreground);
  font-size: 0.75rem;
  font-weight: 700;
  line-height: 1;
}

@media (width >= 640px) {
  .showcase-filter-bar {
    margin-inline: -32px;
    padding-inline: 32px;
  }
}

@media (width >= 1024px) {
  .showcase-filter-bar {
    display: contents;
  }

  .showcase-count {
    flex: none;
    grid-area: count;
    align-self: end;
    justify-self: end;
  }

  .showcase-filters-button {
    display: none;
  }
}
</style>
