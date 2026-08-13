<script setup lang="ts">
import type { ResolvedPhoto } from '../resolveRenderModel';
import { computed } from 'vue';

const props = defineProps<{ photo: ResolvedPhoto }>();
const position = (offset: number, size: number): string =>
  size === 1 ? '50%' : `${(offset / (1 - size)) * 100}%`;

const imageStyle = computed(() => {
  const crop = props.photo.crop;
  if (crop === undefined) return { objectFit: 'cover' as const };
  return {
    objectFit: 'cover' as const,
    objectPosition: [
      position(crop.x, crop.width),
      position(crop.y, crop.height),
    ].join(' '),
  };
});
</script>

<template>
  <img
    :src="photo.url"
    alt=""
    class="resume-photo"
    :style="imageStyle"
  >
</template>
