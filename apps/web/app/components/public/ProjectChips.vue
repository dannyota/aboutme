<script setup lang="ts">
defineProps<{
  readonly technologies: readonly string[];
  readonly more?: number;
  readonly label?: string;
}>();
</script>

<template>
  <ul
    :aria-label="label"
    class="project-chips"
  >
    <li
      v-for="(technology, index) in technologies"
      :key="index"
    >
      {{ technology }}
    </li>
    <li
      v-if="more !== undefined"
      class="project-chips-more"
    >
      {{ `+${more}` }}
    </li>
  </ul>
</template>
