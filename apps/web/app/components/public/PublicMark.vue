<script setup lang="ts">
import {
  MARK_BOWL,
  MARK_HEIGHT,
  MARK_OUTER_RING,
  MARK_STEM,
  MARK_TILT,
  MARK_VIEW_BOX,
  MARK_WIDTH,
} from '../app/sealMark';

// The public page bar's brand mark (docs/design/public-page-theme.md, "Page
// bar"): the seal mark alone, without the hairline inner ring the logo drops
// at 24 px and below (docs/design/ui/identity-and-seal.md, "Logo"). The
// public page loads no Tailwind, so the stroke takes its color from the
// --toolbar-mark custom property the public page's own style block declares,
// through currentColor, rather than a utility class.
const HEIGHT = 20;
const WIDTH = Math.round((HEIGHT * MARK_WIDTH) / MARK_HEIGHT);
</script>

<template>
  <svg
    aria-hidden="true"
    class="public-mark"
    focusable="false"
    :height="HEIGHT"
    :viewBox="MARK_VIEW_BOX"
    :width="WIDTH"
    xmlns="http://www.w3.org/2000/svg"
  >
    <g
      fill="none"
      stroke="currentColor"
      :transform="MARK_TILT"
    >
      <circle
        :cx="MARK_OUTER_RING.cx"
        :cy="MARK_OUTER_RING.cy"
        :r="MARK_OUTER_RING.r"
        :stroke-width="MARK_OUTER_RING.strokeWidth"
      />
      <g
        stroke-linecap="round"
        :stroke-width="MARK_STEM.strokeWidth"
      >
        <circle
          :cx="MARK_BOWL.cx"
          :cy="MARK_BOWL.cy"
          :r="MARK_BOWL.r"
        />
        <path :d="MARK_STEM.d" />
      </g>
    </g>
  </svg>
</template>
