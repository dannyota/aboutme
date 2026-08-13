<script setup lang="ts">
import type { Customization, DateRange } from '@aboutme/schema';
import { computed } from 'vue';

import { formatDateRange } from '../formatDate';

const props = defineProps<{
  dates: DateRange;
  format: Customization['dateFormat'];
}>();
const text = computed(() => formatDateRange(props.dates, props.format));
</script>

<template>
  <span class="date-range">{{ text }}</span>
</template>
