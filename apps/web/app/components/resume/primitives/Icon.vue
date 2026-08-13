<script setup lang="ts">
import { computed } from 'vue';

import { iconFor } from '../icons';

const props = defineProps<{ iconKey: string }>();
const component = computed(() => iconFor(props.iconKey));
</script>

<template>
  <component
    :is="component"
    v-if="component"
    aria-hidden="true"
    class="resume-icon"
    :size="16"
    :stroke-width="1.75"
  />
</template>
