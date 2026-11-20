<script setup lang="ts">
import type { PublicPagesCopy } from './publicPagesCopy';

defineProps<{
  index: number;
  count: number;
  copy: PublicPagesCopy;
}>();
const emit = defineEmits<{ move: [delta: number] }>();
</script>

<template>
  <div class="public-pager">
    <button
      type="button"
      class="public-pager-button"
      data-direction="previous"
      :aria-label="copy.previous"
      :disabled="index <= 0"
      @click="emit('move', -1)"
    >
      <svg
        aria-hidden="true"
        focusable="false"
        width="20"
        height="20"
        viewBox="0 0 24 24"
        fill="none"
        stroke="currentColor"
        stroke-width="2"
        stroke-linecap="round"
        stroke-linejoin="round"
      ><path d="m15 18-6-6 6-6" /></svg>
    </button>
    <p
      class="public-pager-status"
      aria-hidden="true"
    >
      <span class="public-pager-position">{{ index + 1 }} / {{ count }}</span>
      <span class="public-pager-dots">
        <span
          v-for="dot in count"
          :key="dot"
          class="public-pager-dot"
          :data-current="dot - 1 === index ? '' : undefined"
        />
      </span>
    </p>
    <button
      type="button"
      class="public-pager-button"
      data-direction="next"
      :aria-label="copy.next"
      :disabled="index >= count - 1"
      @click="emit('move', 1)"
    >
      <svg
        aria-hidden="true"
        focusable="false"
        width="20"
        height="20"
        viewBox="0 0 24 24"
        fill="none"
        stroke="currentColor"
        stroke-width="2"
        stroke-linecap="round"
        stroke-linejoin="round"
      ><path d="m9 18 6-6-6-6" /></svg>
    </button>
  </div>
</template>
