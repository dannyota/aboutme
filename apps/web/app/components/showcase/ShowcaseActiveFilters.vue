<script setup lang="ts">
import { computed } from 'vue';

import ActiveFilters from '@/components/app/ActiveFilters.vue';
import type { Locale } from '@/i18n/locale';
import { showcaseCopy } from '@/i18n/showcase';
import {
  activeFilterList,
  type ShowcaseFilterKind,
  type ShowcaseFilters,
} from '@/lib/showcaseQuery';
import { galleryTemplate } from '@/templates/catalog';

const props = defineProps<{
  readonly filters: ShowcaseFilters;
  readonly locale: Locale;
}>();
const emit = defineEmits<{
  'remove': [kind: ShowcaseFilterKind, last: boolean];
  'clear-all': [];
}>();
const copy = computed(() => showcaseCopy[props.locale]);
const chips = computed(() =>
  activeFilterList(props.filters).map((filter) => ({
    kind: filter.kind,
    label: label(filter.kind, filter.value),
  })),
);

function label(kind: ShowcaseFilterKind, value: string): string {
  if (kind === 'role') {
    return copy.value.roles[value as keyof typeof copy.value.roles];
  }
  if (kind === 'lang') {
    return copy.value.languages[value as keyof typeof copy.value.languages];
  }
  return galleryTemplate(value)?.name ?? copy.value.customDesign;
}

function onRemove(kind: string, last: boolean): void {
  emit('remove', kind as ShowcaseFilterKind, last);
}
</script>

<template>
  <ActiveFilters
    :chips="chips"
    :copy="copy"
    prefix="showcase"
    @remove="onRemove"
    @clear-all="emit('clear-all')"
  />
</template>
