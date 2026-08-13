<script setup lang="ts">
import { computed } from 'vue';

const props = defineProps<{
  name: string;
  level?: number;
  style: 'text' | 'tag' | 'bar' | 'dots';
}>();
const visible = computed(
  () => props.level !== undefined && props.style !== 'text',
);
const accessibleName = computed(() => `${props.name}: ${props.level} of 5`);
</script>

<template>
  <span
    v-if="visible"
    class="level-widget"
    :class="`level-${style}`"
    role="img"
    :aria-label="accessibleName"
  >
    <span
      v-if="style === 'tag'"
      aria-hidden="true"
    >{{ level }}/5</span>
    <span
      v-else-if="style === 'bar'"
      class="level-track"
      aria-hidden="true"
    >
      <span
        class="level-fill"
        :style="{ width: `${(level! / 5) * 100}%` }"
      />
    </span>
    <span
      v-else
      class="level-dots"
      aria-hidden="true"
    >
      <span
        v-for="index in 5"
        :key="index"
        class="level-dot"
        :class="{ filled: index <= level! }"
      />
    </span>
  </span>
</template>
