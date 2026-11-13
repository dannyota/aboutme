<script setup lang="ts">
// A project's technologies as a list of chips (docs/design/ui/project-pages.md,
// "Accessibility and fit"). Technology names are not language text, so the
// list never carries the project's lang.
defineProps<{
  readonly technologies: readonly string[];
  /** The count a list row leaves out, shown as “+n”. */
  readonly more?: number;
  /** The list's accessible name where no heading names it. */
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
