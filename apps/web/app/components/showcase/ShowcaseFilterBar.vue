<script setup lang="ts">
import { computed, ref } from 'vue';

import FilterBar from '@/components/app/FilterBar.vue';
import type { Locale } from '@/i18n/locale';
import { showcaseCopy } from '@/i18n/showcase';

const props = defineProps<{
  readonly locale: Locale;
  readonly open: boolean;
  readonly text: string;
  readonly head: string;
  readonly tail: string;
  readonly announceKey: number;
  readonly activeCount: number;
}>();
const emit = defineEmits<{ 'update:open': [open: boolean] }>();
const copy = computed(() => showcaseCopy[props.locale]);
const bar = ref<{ focusFilters(): void } | null>(null);
defineExpose({ focusFilters: () => bar.value?.focusFilters() });
</script>

<template>
  <FilterBar
    ref="bar"
    :active-count="activeCount"
    :announce-key="announceKey"
    :copy="copy"
    :head="head"
    :open="open"
    :tail="tail"
    :text="text"
    prefix="showcase"
    @update:open="emit('update:open', $event)"
  >
    <slot />
  </FilterBar>
</template>
