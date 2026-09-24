<script setup lang="ts">
/**
 * `SheetThumbnail` — page one of a resume, rendered by the shared renderer
 * and scaled to its box. With a width it has that size; without one it fills
 * its container's width and measures itself. The render mounts only while
 * the sheet is near the viewport. The picture is decorative: the surrounding
 * card names what it shows.
 */
import type { Resume } from '@aboutme/schema';
import { computed, onBeforeUnmount, onMounted, ref } from 'vue';

import ResumeDocument from '../resume/ResumeDocument.vue';

const props = defineProps<{
  readonly document?: Resume;
  readonly lng: string;
  /** Width in CSS pixels; absent means the container's width. */
  readonly width?: number;
}>();

/** A4 at 96 dpi: 210 × 297 mm is 794 × 1123 CSS pixels. */
const SHEET_WIDTH = 794;
const SHEET_HEIGHT = 1123;

const root = ref<HTMLElement>();
const near = ref(false);
const measured = ref<number>();
let observer: IntersectionObserver | undefined;
let resizer: ResizeObserver | undefined;

const width = computed(() => props.width ?? measured.value);
const box = computed(() => props.width === undefined
  ? { aspectRatio: `${SHEET_WIDTH} / ${SHEET_HEIGHT}` }
  : {
      width: `${props.width}px`,
      height: `${Math.round((props.width * SHEET_HEIGHT) / SHEET_WIDTH)}px`,
    });
const visible = computed(() =>
  near.value && props.document !== undefined && width.value !== undefined);
const page = computed(() => ({
  zoom: String((width.value ?? SHEET_WIDTH) / SHEET_WIDTH),
}));
const context = computed(() => ({
  lng: props.lng,
  mode: 'continuous' as const,
  // The thumbnail is decorative (aria-hidden and inert above); the name
  // is visual only, so the page's h1 count stays one per template card.
  nameHeading: 'p' as const,
}));

onMounted(() => {
  const element = root.value;
  if (element === undefined) return;
  if (props.width === undefined && typeof ResizeObserver !== 'undefined') {
    resizer = new ResizeObserver(() => {
      measured.value = element.clientWidth;
    });
    resizer.observe(element);
    measured.value = element.clientWidth;
  }
  // Without an observer (old browsers, tests) the sheet stays blank.
  if (typeof IntersectionObserver === 'undefined') return;
  observer = new IntersectionObserver(
    (entries) => {
      near.value = entries.some((entry) => entry.isIntersecting);
    },
    { rootMargin: '300px 0px' },
  );
  observer.observe(element);
});

onBeforeUnmount(() => {
  observer?.disconnect();
  resizer?.disconnect();
});
</script>

<template>
  <div
    ref="root"
    aria-hidden="true"
    class="sheet-thumbnail overflow-hidden rounded-[var(--radius-sheet)]
      bg-white shadow-[var(--shadow-paper)]"
    data-sheet-thumbnail
    inert
    :style="box"
  >
    <div
      v-if="visible"
      class="sheet-thumbnail__page"
      data-sheet-thumbnail-render
      :style="page"
    >
      <ResumeDocument
        :context="context"
        :document="document!"
      />
    </div>
  </div>
</template>

<style scoped>
.sheet-thumbnail__page {
  width: 210mm;
  min-height: 297mm;
  pointer-events: none;
}
</style>
