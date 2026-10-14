<script setup lang="ts">
import { ListFilter } from '@lucide/vue';
import { computed, ref } from 'vue';

import { Button } from '@/components/ui/button';
import { Sheet, SheetTrigger } from '@/components/ui/sheet';
import type { FilterControlCopy } from '@/i18n/filters';

const props = defineProps<{
  readonly prefix: 'showcase' | 'gallery';
  readonly copy: FilterControlCopy;
  readonly layout?: 'showcase' | 'library';
  readonly disabled?: boolean;
  readonly open: boolean;
  /** The count line text; empty before the first load (a skeleton). */
  readonly text: string;
  /** The bold lead, such as "1,234 resumes"; empty before any total. */
  readonly head?: string;
  /** The order note after the head and ` · `; unused when `head` is empty. */
  readonly tail?: string;
  /** Changes when a filter change settles, so the region announces again. */
  readonly announceKey: number;
  readonly activeCount: number;
}>();
const emit = defineEmits<{ 'update:open': [open: boolean] }>();
const bar = ref<HTMLElement | null>(null);
const tailText = computed(() => props.tail ? ` · ${props.tail}` : '');

defineExpose({
  focusFilters: (): void => {
    bar.value
      ?.querySelector<HTMLElement>(
        `[data-action="${props.prefix}-filters-open"]`,
      )
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
      :data-testid="`${prefix}-filter-bar`"
      :class="{ 'library-filter-bar': layout === 'library' }"
    >
      <p
        aria-atomic="true"
        class="showcase-count"
        :data-testid="`${prefix}-count`"
        role="status"
      >
        <span
          v-if="text === ''"
          class="inline-block h-5 w-24 animate-pulse rounded-md bg-primary/10
            align-top"
          data-slot="skeleton"
        />
        <span
          v-else
          :key="announceKey"
        >
          <template v-if="head">
            <strong class="showcase-count__head">{{ head }}</strong>{{
              tailText }}
          </template>
          <template v-else>{{ text }}</template>
        </span>
      </p>
      <SheetTrigger as-child>
        <Button
          :aria-label="
            activeCount > 0 ? copy.filtersActive(activeCount) : undefined
          "
          class="showcase-filters-button"
          :class="{ 'is-active': activeCount > 0 }"
          :data-action="`${prefix}-filters-open`"
          :disabled="disabled"
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
            :data-testid="`${prefix}-filters-badge`"
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
    justify-self: stretch;
    text-align: right;
  }

  .showcase-filters-button {
    display: none;
  }
}
</style>

<style>
@media (width < 1024px) {
  html[data-ui="app"].filter-bar-scroll-padding {
    scroll-padding-top: 68px;
  }
}
</style>

<style scoped>
.library-filter-bar {
  margin-top: 16px;
}
.library-filter-bar .showcase-count {
  color: var(--foreground);
  font-weight: 600;
}
@media (width >= 1024px) {
  .library-filter-bar {
    position: static;
    display: flex;
    height: auto;
    margin: 24px 0 0;
    padding: 0;
    border: 0;
    background: transparent;
  }
  .library-filter-bar .showcase-count {
    text-align: left;
  }
}
</style>
