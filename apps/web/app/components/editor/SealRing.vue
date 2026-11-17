<script setup lang="ts">
import {
  MARK_BOWL,
  MARK_OUTER_RING,
  MARK_STEM,
  MARK_TILT,
  MARK_VIEW_BOX,
} from '../app/sealMark';
</script>

<template>
  <svg
    aria-hidden="true"
    class="size-4"
    data-ring-a
    focusable="false"
    height="16"
    :viewBox="MARK_VIEW_BOX"
    width="15"
    xmlns="http://www.w3.org/2000/svg"
  >
    <g
      fill="none"
      stroke="currentColor"
      stroke-linecap="round"
      :stroke-width="MARK_STEM.strokeWidth"
      :transform="MARK_TILT"
    >
      <circle
        :cx="MARK_OUTER_RING.cx"
        :cy="MARK_OUTER_RING.cy"
        :r="MARK_OUTER_RING.r"
      />
      <circle
        :cx="MARK_BOWL.cx"
        :cy="MARK_BOWL.cy"
        :r="MARK_BOWL.r"
      />
      <path :d="MARK_STEM.d" />
    </g>
  </svg>
</template>
