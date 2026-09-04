<script setup lang="ts">
import { computed } from 'vue';

import AppSeal from './AppSeal.vue';

const props = defineProps<{
  state: 'saved' | 'saving' | 'failed' | 'draft' | 'public';
  link?: string;
}>();

const publicLink = computed(() => {
  if (props.state === 'public' && props.link === undefined) {
    throw new Error('StateMark public state requires a link.');
  }
  return props.link ?? '';
});
</script>

<template>
  <span
    :aria-live="state === 'saving' ? 'polite' : undefined"
    :class="[
      'inline-flex items-center gap-1.5 text-sm',
      state === 'failed' ? 'text-destructive' : 'text-muted-foreground',
    ]"
    :data-state-mark="state"
    :role="
      state === 'failed' ? 'alert' : state === 'public' ? undefined : 'status'
    "
  >
    <template v-if="state === 'saved'">
      <svg
        aria-hidden="true"
        data-state-glyph="saved"
        fill="none"
        height="14"
        viewBox="0 0 14 14"
        width="14"
      >
        <path
          d="m2 10.75 1.1-3.3L9.8.75l2.45 2.45-6.7 6.7L2 10.75Z"
          stroke="currentColor"
          stroke-linejoin="round"
          stroke-width="1.25"
        />
        <path
          d="m7.75 10.25 1.4 1.4 2.85-3.1"
          stroke="currentColor"
          stroke-linecap="round"
          stroke-linejoin="round"
          stroke-width="1.25"
        />
      </svg>
      <span>Saved</span>
    </template>
    <template v-else-if="state === 'saving'"> Saving… </template>
    <template v-else-if="state === 'failed'"> Save failed </template>
    <template v-else-if="state === 'draft'"> Draft </template>
    <template v-else>
      <AppSeal
        :link="publicLink"
        size="mark"
      />
      <a
        data-public-link
        :href="publicLink"
      >aboutme.vn{{ publicLink }}</a>
    </template>
  </span>
</template>

<style>
@keyframes aboutme-stamp-land {
  from {
    opacity: 0;
    transform: scale(1.12);
  }

  to {
    opacity: 1;
    transform: scale(1);
  }
}

@keyframes aboutme-stamp-lift {
  from {
    opacity: 1;
  }

  to {
    opacity: 0;
  }
}

[data-stamp='landing'] {
  animation: aboutme-stamp-land 180ms ease-out;
  transform-origin: center;
}

[data-stamp='lifting'] {
  animation: aboutme-stamp-lift 120ms ease-in forwards;
}

@media (prefers-reduced-motion: reduce) {
  [data-stamp] {
    animation: none;
  }
}
</style>
