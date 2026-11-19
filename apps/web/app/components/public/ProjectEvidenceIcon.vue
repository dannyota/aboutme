<script setup lang="ts">
defineProps<{
  readonly kind: 'repository' | 'pullRequest' | 'release' | 'confirmed';
}>();

const TAG = 'M12.586 2.586A2 2 0 0 0 11.172 2H4a2 2 0 0 0-2 2v7.172a2 2 0 0 0 '
  + '.586 1.414l8.704 8.704a2.426 2.426 0 0 0 3.42 0l6.58-6.58a2.426 2.426 0 '
  + '0 0 0-3.42z';
const BADGE = 'M3.85 8.62a4 4 0 0 1 4.78-4.77 4 4 0 0 1 6.74 0 4 4 0 0 1 4.78 '
  + '4.78 4 4 0 0 1 0 6.74 4 4 0 0 1-4.77 4.78 4 4 0 0 1-6.75 0 4 4 0 0 '
  + '1-4.78-4.77 4 4 0 0 1 0-6.76Z';
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
    <g v-else-if="kind === 'pullRequest'">
      <circle
        cx="18"
        cy="18"
        r="3"
      />
      <circle
        cx="6"
        cy="6"
        r="3"
      />
      <path d="M13 6h3a2 2 0 0 1 2 2v7" />
      <line
        x1="6"
        x2="6"
        y1="9"
        y2="21"
      />
    </g>
    <g v-else-if="kind === 'release'">
      <path :d="TAG" />
      <circle
        cx="7.5"
        cy="7.5"
        fill="currentColor"
        r=".5"
      />
    </g>
    <g v-else>
      <path :d="BADGE" />
      <path d="m16 9-5.5 5.5L8 12" />
    </g>
  </svg>
</template>
