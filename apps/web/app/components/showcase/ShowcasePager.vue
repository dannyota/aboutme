<script setup lang="ts">
import { ChevronLeft, ChevronRight } from '@lucide/vue';
import { computed, useId } from 'vue';

import { buttonVariants } from '@/components/ui/button';
import type { Locale } from '@/i18n/locale';
import { showcaseCopy } from '@/i18n/showcase';
import { type ShowcaseFilters, showcaseRouteQuery } from '@/lib/showcaseQuery';
import { cn } from '@/lib/utils';

// Previous, the page status, and Next. A link keeps the active filters and
// sets `?page=`; at either end the control is a span that is not a stop
// (docs/design/ui/landing-and-library.md, Community showcase, Pager).
const props = defineProps<{
  readonly page: number;
  readonly pageCount: number;
  readonly filters: ShowcaseFilters;
  readonly locale: Locale;
}>();
const emit = defineEmits<{ navigate: [] }>();
const copy = computed(() => showcaseCopy[props.locale]);
const statusId = `showcase-pager-${useId()}`;
const buttonClass = cn(
  buttonVariants({ variant: 'outline' }),
  'showcase-pager__button',
);
// Past the end, Previous lands on the last real page.
const previousPage = computed(() =>
  Math.max(1, Math.min(props.page - 1, props.pageCount)));
const canGoPrevious = computed(() => props.page > 1);
const canGoNext = computed(() => props.page < props.pageCount);

function target(page: number) {
  return {
    path: '/showcase',
    query: showcaseRouteQuery({ ...props.filters, page }),
  };
}
</script>

<template>
  <nav
    :aria-labelledby="statusId"
    class="showcase-pager"
    data-testid="showcase-pager"
  >
    <NuxtLink
      v-if="canGoPrevious"
      :class="buttonClass"
      data-action="showcase-previous"
      :to="target(previousPage)"
      @click="emit('navigate')"
    >
      <ChevronLeft aria-hidden="true" />
      {{ copy.previous }}
    </NuxtLink>
    <span
      v-else
      aria-disabled="true"
      :class="cn(buttonClass, 'opacity-50')"
      data-action="showcase-previous"
    >
      <ChevronLeft aria-hidden="true" />
      {{ copy.previous }}
    </span>
    <p
      :id="statusId"
      class="showcase-pager__status"
    >
      {{ copy.pageStatus(page, pageCount) }}
    </p>
    <NuxtLink
      v-if="canGoNext"
      :class="buttonClass"
      data-action="showcase-next"
      :to="target(page + 1)"
      @click="emit('navigate')"
    >
      {{ copy.next }}
      <ChevronRight aria-hidden="true" />
    </NuxtLink>
    <span
      v-else
      aria-disabled="true"
      :class="cn(buttonClass, 'opacity-50')"
      data-action="showcase-next"
    >
      {{ copy.next }}
      <ChevronRight aria-hidden="true" />
    </span>
  </nav>
</template>

<style scoped>
.showcase-pager {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 12px;
  margin-top: 40px;
}

.showcase-pager__status {
  grid-column: 1 / -1;
  grid-row: 1;
  color: var(--muted-foreground);
  font-size: 0.875rem;
  font-variant-numeric: tabular-nums;
  text-align: center;
}

@media (width >= 641px) {
  .showcase-pager {
    display: flex;
    justify-content: center;
    align-items: center;
  }
}
</style>
