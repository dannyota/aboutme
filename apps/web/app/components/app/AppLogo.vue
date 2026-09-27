<script setup lang="ts">
import { computed } from 'vue';

// The aboutme.vn logo (DESIGN.md, ADR 0065): a seal mark, two rings and a
// single-story "a" tilted -8 degrees like the public stamp, then the
// lowercase wordmark in the text color with only the dot of ".vn" in seal
// red. Letters are stroked paths, so no font is needed, and nothing uses a
// gradient or an id, so any number of logos can share a page.
const props = withDefaults(
  defineProps<{ size?: 'sm' | 'md' | 'lg'; markOnly?: boolean }>(),
  { size: 'md', markOnly: false },
);

// Path data in a 170 x 32 box; the mark fills the first 30 units.
const BOWL = 'a4.85 4.85 0 1 0 9.7 0a4.85 4.85 0 1 0-9.7 0';
const WORDMARK = `M36.4 19.05${BOWL}M46.1 14.2v9.7M51.5 8.7v15.2m0-4.85${BOWL}`
  + `m14.9 0${BOWL}M81.5 14.2v5.6a4.1 4.1 0 0 0 8.2 0m0-5.6v9.7`
  + 'M95.1 10.1v11.3a2.5 2.5 0 0 0 2.5 2.5h.7m-3.2-9.7h3.2'
  + 'M103.5 23.9v-9.7m0 3.7a3.7 3.7 0 0 1 7.4 0v6m0-6a3.7 3.7 0 0 1 '
  + '7.4 0v6M124 19.95h9.5a4.85 4.85 0 1 0-3.1 3.65'
  + 'M145.5 14.2l4.85 9.7l4.85-9.7M160.4 23.9v-9.7m0 3.7a3.7 3.7 0 0 1 '
  + '7.4 0v6';

const height = computed(() => ({ sm: 24, md: 32, lg: 48 })[props.size]);
const viewWidth = computed(() => (props.markOnly ? 30 : 170));
const sizeClass = computed(
  () => ({ sm: 'h-6', md: 'h-8', lg: 'h-12' })[props.size],
);
</script>

<template>
  <svg
    aria-label="aboutme.vn"
    :class="['w-auto shrink-0', sizeClass]"
    :data-logo-size="size"
    :height="height"
    role="img"
    :viewBox="`0 0 ${viewWidth} 32`"
    :width="(height * viewWidth) / 32"
    xmlns="http://www.w3.org/2000/svg"
  >
    <g
      class="text-seal-text forced-colors:text-[CanvasText]"
      data-logo-part="mark"
      fill="none"
      stroke="currentColor"
      transform="rotate(-8 15 16)"
    >
      <circle
        cx="15"
        cy="16"
        data-logo-ring="outer"
        r="13.4"
        stroke-width="2.6"
      />
      <circle
        cx="15"
        cy="16"
        data-logo-ring="inner"
        r="9.6"
        stroke-width="1"
      />
      <g
        stroke-linecap="round"
        stroke-width="3.2"
      >
        <circle
          cx="13.4"
          cy="17.2"
          r="3.7"
        />
        <path d="M17.1 13.2v7.8" />
      </g>
    </g>
    <g
      v-if="!markOnly"
      data-logo-part="wordmark"
    >
      <path
        :d="WORDMARK"
        fill="none"
        stroke="currentColor"
        stroke-linecap="round"
        stroke-linejoin="round"
        stroke-width="4.2"
      />
      <circle
        class="text-seal-text forced-colors:text-[CanvasText]"
        cx="139.6"
        cy="23.2"
        data-logo-part="dot"
        fill="currentColor"
        r="2.8"
      />
    </g>
  </svg>
</template>
