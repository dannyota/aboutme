<script setup lang="ts">
import { computed } from 'vue';

import type { Locale } from '@/i18n/locale';

import { SEAL_WORD, sealLayout, STAMP_HEIGHT } from './sealLayout';

// The public seal (DESIGN.md, ADR 0065). The stamp is a rounded ticket: the
// logo's seal mark (outer ring and "a") and the word PUBLIC (CÔNG KHAI in
// Vietnamese) on top, a hairline, then the public link. It grows with the
// link so a slug never overlaps itself (sealLayout.ts). The mark is a 20 px
// seal tile with a check that always sits beside the public link.
const props = withDefaults(
  defineProps<{
    link: string;
    /** The stamp's word follows the page language (PUBLIC or CÔNG KHAI). */
    locale: Locale;
    label?: string;
    size?: 'mark' | 'stamp';
    rotate?: number;
  }>(),
  {
    size: 'stamp',
    rotate: -6,
  },
);

const word = computed(() => SEAL_WORD[props.locale]);
const linkText = computed(() => `aboutme.vn${props.link}`);
const layout = computed(() =>
  sealLayout(props.link, props.locale, props.rotate));

const logoTransform = computed(() =>
  `translate(${layout.value.headX} 9.5) scale(0.8125) rotate(-8 15 16)`);
</script>

<template>
  <svg
    v-if="size === 'stamp'"
    :aria-label="label ?? `Public at aboutme.vn${link}`"
    class="h-auto max-w-full text-seal-text"
    data-app-seal="stamp"
    :height="layout.svgHeight"
    role="img"
    :viewBox="layout.viewBox"
    :width="layout.svgWidth"
    xmlns="http://www.w3.org/2000/svg"
  >
    <g
      data-seal-stamp
      :transform="`rotate(${rotate} ${layout.width / 2} ${STAMP_HEIGHT / 2})`"
    >
      <!-- An opaque ground under the tint, so an edge or a line the stamp
           sits on never shows through it. -->
      <rect
        data-seal-ground
        fill="var(--card)"
        :height="STAMP_HEIGHT - 2.5"
        rx="14"
        :width="layout.width - 2.5"
        x="1.25"
        y="1.25"
      />
      <rect
        data-seal-ticket
        fill="currentColor"
        fill-opacity=".07"
        :height="STAMP_HEIGHT - 2.5"
        rx="14"
        stroke="currentColor"
        stroke-width="2.5"
        :width="layout.width - 2.5"
        x="1.25"
        y="1.25"
      />
      <rect
        data-seal-inner
        fill="none"
        :height="STAMP_HEIGHT - 12"
        rx="10"
        stroke="currentColor"
        stroke-dasharray="2 3"
        stroke-opacity=".45"
        stroke-width="1"
        :width="layout.width - 12"
        x="6"
        y="6"
      />
      <g
        data-seal-logo
        fill="none"
        stroke="currentColor"
        :transform="logoTransform"
      >
        <circle
          cx="15"
          cy="16"
          r="13.4"
          stroke-width="3.2"
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
      <text
        data-seal-word
        fill="currentColor"
        font-size="20"
        font-weight="800"
        letter-spacing="2.8"
        :x="layout.wordX"
        y="31"
      >{{ word }}</text>
      <line
        stroke="currentColor"
        stroke-opacity=".35"
        stroke-width="1"
        x1="18"
        :x2="layout.width - 18"
        y1="42"
        y2="42"
      />
      <text
        data-seal-link
        fill="currentColor"
        font-size="11"
        font-weight="600"
        :lengthAdjust="layout.linkLength === undefined
          ? undefined
          : 'spacingAndGlyphs'"
        text-anchor="middle"
        :textLength="layout.linkLength"
        :x="layout.width / 2"
        y="58"
      >{{ linkText }}</text>
    </g>
  </svg>
  <svg
    v-else
    :aria-label="label ?? `Public at aboutme.vn${link}`"
    class="text-seal"
    data-app-seal="mark"
    height="20"
    role="img"
    viewBox="0 0 20 20"
    width="20"
    xmlns="http://www.w3.org/2000/svg"
  >
    <rect
      fill="currentColor"
      height="20"
      rx="6"
      width="20"
    />
    <path
      d="m5.6 10.2 2.8 2.8 5.9-6.1"
      data-seal-check
      fill="none"
      stroke="var(--seal-foreground)"
      stroke-linecap="round"
      stroke-linejoin="round"
      stroke-width="2"
    />
  </svg>
</template>
