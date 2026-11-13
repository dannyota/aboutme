<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref } from 'vue';

// CSS zoom lays text out at the zoomed size, and WebKit clamps small fonts.
const props = defineProps<{
  scale: number;
}>();

const content = ref<HTMLElement | null>(null);
const contentWidthPx = ref(0);
const contentHeightPx = ref(0);
let observer: ResizeObserver | undefined;

onMounted(() => {
  const element = content.value;
  if (element === null) return;
  contentWidthPx.value = element.offsetWidth;
  contentHeightPx.value = element.offsetHeight;
  if (typeof ResizeObserver === 'undefined') return;
  observer = new ResizeObserver(([entry]) => {
    if (entry === undefined) return;
    contentWidthPx.value = entry.contentRect.width;
    contentHeightPx.value = entry.contentRect.height;
  });
  observer.observe(element);
});

onBeforeUnmount(() => {
  observer?.disconnect();
});
</script>

<template>
  <div
    class="scaled-sheet"
    :style="{
      width: `${contentWidthPx * props.scale}px`,
      height: `${contentHeightPx * props.scale}px`,
    }"
  >
    <div
      ref="content"
      class="scaled-sheet-content"
      :style="{ transform: `scale(${props.scale})` }"
    >
      <slot />
    </div>
  </div>
</template>

<style>
.scaled-sheet-content {
  width: max-content;
  transform-origin: 0 0;
}
</style>
