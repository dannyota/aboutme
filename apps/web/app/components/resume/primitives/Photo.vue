<script setup lang="ts">
import type { ResolvedPhoto } from '../resolveRenderModel';
import { computed } from 'vue';

const props = defineProps<{ photo: ResolvedPhoto }>();

const percent = (value: number): string =>
  `${Math.round(value * 100 * 1_000_000) / 1_000_000}%`;

// The image box maps the whole image onto the frame so the crop rectangle
// fills it; object-fit: cover (in the stylesheet) keeps the image aspect, so a
// crop that is square in pixels is exact and no crop is ever stretched.
const imageStyle = computed(() => {
  const crop = props.photo.crop ?? { x: 0, y: 0, width: 1, height: 1 };
  return {
    left: percent(-crop.x / crop.width),
    top: percent(-crop.y / crop.height),
    width: percent(1 / crop.width),
    height: percent(1 / crop.height),
  };
});
</script>

<template>
  <span class="resume-photo">
    <img
      :src="photo.url"
      alt=""
      class="resume-photo-image"
      :style="imageStyle"
    >
  </span>
</template>
