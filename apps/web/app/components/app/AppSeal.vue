<script setup lang="ts">
import { computed, useId } from 'vue';

const props = withDefaults(
  defineProps<{
    link: string;
    size?: 'mark' | 'stamp';
    rotate?: number;
  }>(),
  {
    size: 'stamp',
    rotate: -8,
  },
);

const ringPathId = `app-seal-ring-${useId()}`;
const ringText = computed(
  () => `PUBLIC RESUME · ABOUTME.VN${props.link.toUpperCase()} · `,
);
</script>

<template>
  <svg
    :aria-label="`Public at aboutme.vn${link}`"
    :data-app-seal="size"
    :height="size === 'stamp' ? 96 : 20"
    :viewBox="size === 'stamp' ? '0 0 96 96' : '0 0 20 20'"
    :width="size === 'stamp' ? 96 : 20"
    role="img"
    style="color: var(--seal)"
    xmlns="http://www.w3.org/2000/svg"
  >
    <g
      v-if="size === 'stamp'"
      data-seal-stamp
      :transform="`rotate(${rotate} 48 48)`"
    >
      <defs>
        <path
          :id="ringPathId"
          d="M 48 48 m -35 0 a 35 35 0 1 1 70 0 a 35 35 0 1 1 -70 0"
        />
      </defs>
      <circle
        cx="48"
        cy="48"
        data-seal-ring="outer"
        fill="none"
        r="45"
        stroke="currentColor"
        stroke-width="2"
      />
      <circle
        cx="48"
        cy="48"
        data-seal-ring="inner"
        fill="none"
        r="39"
        stroke="currentColor"
        stroke-width="1"
      />
      <text
        data-seal-ring-label
        fill="currentColor"
        font-size="9"
        letter-spacing="0.08em"
      >
        <textPath
          data-seal-ring-text
          :href="`#${ringPathId}`"
          startOffset="50%"
          text-anchor="middle"
        >
          {{ ringText }}
        </textPath>
      </text>
      <text
        x="48"
        y="53"
        data-seal-center
        fill="currentColor"
        font-size="14"
        font-weight="600"
        text-anchor="middle"
      >
        aboutme
      </text>
    </g>
    <g
      v-else
      data-seal-mark
    >
      <circle
        cx="10"
        cy="10"
        fill="currentColor"
        r="10"
      />
      <path
        d="m5.5 10.25 2.75 2.75 6.25-6.25"
        data-seal-check
        fill="none"
        stroke="var(--seal-foreground)"
        stroke-linecap="round"
        stroke-linejoin="round"
        stroke-width="2"
      />
    </g>
  </svg>
</template>
