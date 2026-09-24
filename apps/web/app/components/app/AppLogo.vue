<script setup lang="ts">
import { computed, useId } from 'vue';

// The aboutme logo (DESIGN.md, ADR 0050): a document whose body forms a
// lowercase "a", then the wordmark with "about" in the text color and "me" in
// the brand gradient. Letters are stroked paths, so no font is needed.
const props = withDefaults(
  defineProps<{ size?: 'sm' | 'md' | 'lg'; markOnly?: boolean }>(),
  { size: 'md', markOnly: false },
);

// Path data in a 136 x 32 box; the mark fills the first 29 units.
const MARK = 'M0 9.5A9.5 9.5 0 0 1 9.5 0h9.2L29 10.3v19a2.2 2.2 0 0 1-2.2 '
  + '2.2h-1.3a2.2 2.2 0 0 1-2.2-2.2v-7.9h-2.9v7a3.6 3.6 0 0 1-3.6 3.6H8a8 8 '
  + '0 0 1-8-8Z';
const BOWL = 'a4.85 4.85 0 1 0 9.7 0a4.85 4.85 0 1 0-9.7 0';
const ABOUT = `M36.4 19.05${BOWL}M46.1 14.2v9.7M51.5 8.7v15.2m0-4.85${BOWL}`
  + `m14.9 0${BOWL}M81.5 14.2v5.6a4.1 4.1 0 0 0 8.2 0m0-5.6v9.7`
  + 'M95.1 10.1v11.3a2.5 2.5 0 0 0 2.5 2.5h.7m-3.2-9.7h3.2';
const ME = 'M103.5 23.9v-9.7m0 3.7a3.7 3.7 0 0 1 7.4 0v6m0-6a3.7 3.7 0 0 1 '
  + '7.4 0v6M124 19.95h9.5a4.85 4.85 0 1 0-3.1 3.65';

// Header and page can both render a logo, so every id is per instance.
const id = useId();
const markFill = `app-logo-fill-${id}`;
const markCut = `app-logo-cut-${id}`;
const meStroke = `app-logo-me-${id}`;

const height = computed(() => ({ sm: 24, md: 32, lg: 48 })[props.size]);
const viewWidth = computed(() => (props.markOnly ? 29 : 136));
const sizeClass = computed(
  () => ({ sm: 'h-6', md: 'h-8', lg: 'h-12' })[props.size],
);
</script>

<template>
  <svg
    aria-label="aboutme"
    :class="['w-auto shrink-0', sizeClass]"
    :data-logo-size="size"
    :height="height"
    role="img"
    :viewBox="`0 0 ${viewWidth} 32`"
    :width="(height * viewWidth) / 32"
    xmlns="http://www.w3.org/2000/svg"
  >
    <defs>
      <linearGradient :id="markFill" x2="1" y2="1">
        <stop stop-color="#35c8f5" />
        <stop offset=".5" stop-color="#246bfd" />
        <stop offset="1" stop-color="#4a35f5" />
      </linearGradient>
      <mask :id="markCut" class="forced-color-adjust-none">
        <rect fill="#fff" height="32" width="29" />
        <circle cx="14.3" cy="21.4" r="8.9" />
      </mask>
      <linearGradient
        v-if="!markOnly"
        :id="meStroke"
        gradientUnits="userSpaceOnUse"
        x1="101"
        x2="136"
      >
        <stop class="[stop-color:var(--brand-blue)]" stop-color="#246bfd" />
        <stop
          class="[stop-color:var(--brand-indigo)]"
          offset="1"
          stop-color="#6254ff"
        />
      </linearGradient>
    </defs>
    <g data-logo-part="mark">
      <path
        class="forced-colors:fill-current"
        :d="MARK"
        :fill="`url(#${markFill})`"
        :mask="`url(#${markCut})`"
      />
      <path
        class="forced-colors:fill-[Canvas]"
        d="M18.7 0v7.3a3 3 0 0 0 3 3H29Z"
        fill="#fff"
        fill-opacity=".8"
      />
      <circle
        class="forced-colors:fill-current"
        cx="15.1"
        cy="22.4"
        :fill="`url(#${markFill})`"
        r="4.9"
      />
    </g>
    <g
      v-if="!markOnly"
      data-logo-part="wordmark"
      fill="none"
      stroke-linecap="round"
      stroke-linejoin="round"
      stroke-width="4.2"
    >
      <path
        :d="ABOUT"
        stroke="currentColor"
      />
      <path
        class="forced-colors:stroke-current"
        :d="ME"
        :stroke="`url(#${meStroke})`"
      />
    </g>
  </svg>
</template>
