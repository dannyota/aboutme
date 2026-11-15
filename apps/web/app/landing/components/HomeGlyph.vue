<script setup lang="ts">
defineProps<{ readonly name: 'check' | 'mark' | 'link' | 'saved' }>();

const LINK_PATHS = [
  'M8.5 11.5a3.6 3.6 0 0 0 5.1 0l2.6-2.6a3.6 3.6 0 0 0-5.1-5.1l-.9.9',
  'M11.5 8.5a3.6 3.6 0 0 0-5.1 0l-2.6 2.6a3.6 3.6 0 0 0 5.1 5.1l.9-.9',
] as const;
</script>

<template>
  <svg
    v-if="name === 'check'"
    aria-hidden="true"
    data-glyph="check"
    viewBox="0 0 22 22"
  >
    <circle
      cx="11"
      cy="11"
      fill="var(--brand)"
      r="11"
    />
    <path
      d="M6 11.5l3.4 3.4L16 7.8"
      fill="none"
      stroke="var(--brand-foreground)"
      stroke-linecap="round"
      stroke-linejoin="round"
      stroke-width="2.6"
    />
  </svg>
  <svg
    v-else-if="name === 'mark'"
    aria-hidden="true"
    data-glyph="mark"
    viewBox="0 0 30 32"
  >
    <g
      fill="none"
      stroke="var(--coin-ink)"
      stroke-linecap="round"
      stroke-width="3.4"
      transform="rotate(-8 15 16)"
    >
      <circle
        cx="13.4"
        cy="17.2"
        r="5.4"
      />
      <path d="M19.6 11v12" />
    </g>
  </svg>
  <svg
    v-else-if="name === 'link'"
    aria-hidden="true"
    data-glyph="link"
    viewBox="0 0 20 20"
  >
    <g
      fill="none"
      stroke="var(--coin-ink)"
      stroke-linecap="round"
      stroke-width="2.4"
    >
      <path
        v-for="path in LINK_PATHS"
        :key="path"
        :d="path"
      />
    </g>
  </svg>
  <svg
    v-else
    aria-hidden="true"
    data-glyph="saved"
    viewBox="0 0 16 16"
  >
    <path
      d="M3 8.5l3.2 3.2L13 4.6"
      fill="none"
      stroke="currentColor"
      stroke-linecap="round"
      stroke-linejoin="round"
      stroke-width="2"
    />
  </svg>
</template>
