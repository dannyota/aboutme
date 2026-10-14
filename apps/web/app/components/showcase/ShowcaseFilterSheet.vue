<script setup lang="ts">
import { computed } from 'vue';

import FilterSheet from '@/components/app/FilterSheet.vue';
import ShowcaseFilters from '@/components/showcase/ShowcaseFilters.vue';
import type { Locale } from '@/i18n/locale';
import { showcaseCopy } from '@/i18n/showcase';
import type { ShowcaseFilters as Filters } from '@/lib/showcaseQuery';

const props = defineProps<{
  readonly filters: Filters;
  readonly locale: Locale;
  readonly total: number | null;
  readonly status: string;
  readonly announceKey: number;
}>();
const emit = defineEmits<{
  'change': [filters: Filters];
  'close-auto-focus': [event: Event];
}>();
const copy = computed(() => showcaseCopy[props.locale]);
const resultsLabel = computed(() =>
  props.total === null
    ? copy.value.showResultsLoading
    : copy.value.showResults(props.total),
);
</script>

<template>
  <FilterSheet
    :announce-key="announceKey"
    :copy="copy"
    prefix="showcase"
    :results-label="resultsLabel"
    :status="status"
    @clear="emit('change', {})"
    @close-auto-focus="emit('close-auto-focus', $event)"
  >
    <ShowcaseFilters
      :filters="filters"
      layout="sheet"
      :locale="locale"
      @change="emit('change', $event)"
    />
  </FilterSheet>
</template>
