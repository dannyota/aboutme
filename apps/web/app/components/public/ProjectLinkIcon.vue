<script setup lang="ts">
// Geometry from Lucide's git-branch, external-link and link icons (ISC).
defineProps<{ readonly kind: 'repository' | 'demo' | 'link' }>();
</script>

<template>
  <svg
    aria-hidden="true"
    fill="none"
    focusable="false"
    height="16"
    stroke="currentColor"
    stroke-linecap="round"
    stroke-linejoin="round"
    stroke-width="2"
    viewBox="0 0 24 24"
    width="16"
    xmlns="http://www.w3.org/2000/svg"
  >
    <g v-if="kind === 'repository'">
      <path d="M15 6a9 9 0 0 0-9 9V3" />
      <circle
        cx="18"
        cy="6"
        r="3"
      />
      <circle
        cx="6"
        cy="18"
        r="3"
      />
    </g>
    <g v-else-if="kind === 'demo'">
      <path d="M15 3h6v6" />
      <path d="M10 14 21 3" />
      <path d="M18 13v6a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V8a2 2 0 0 1 2-2h6" />
    </g>
    <g v-else>
      <path d="M10 13a5 5 0 0 0 7.54.54l3-3a5 5 0 0 0-7.07-7.07l-1.72 1.71" />
      <path d="M14 11a5 5 0 0 0-7.54-.54l-3 3a5 5 0 0 0 7.07 7.07l1.71-1.71" />
    </g>
  </svg>
</template>
