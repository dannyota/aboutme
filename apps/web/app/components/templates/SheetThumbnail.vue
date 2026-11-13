<script setup lang="ts">
import type { Resume } from '@aboutme/schema';
import { computed, onBeforeUnmount, onMounted, ref } from 'vue';

import ResumeDocument from '../resume/ResumeDocument.vue';
import ScaledSheet from '../resume/ScaledSheet.vue';

const props = defineProps<{
  readonly document?: Resume;
  readonly lng: string;
  readonly width?: number;
}>();

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
const scale = computed(() => (width.value ?? SHEET_WIDTH) / SHEET_WIDTH);
const context = computed(() => ({
  lng: props.lng,
  mode: 'continuous' as const,
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
    <ScaledSheet
      v-if="visible"
      :scale="scale"
    >
      <div
        class="sheet-thumbnail__page"
        data-sheet-thumbnail-render
      >
        <ResumeDocument
          :context="context"
          :document="document!"
        />
      </div>
    </ScaledSheet>
  </div>
</template>

<style scoped>
.sheet-thumbnail__page {
  width: 210mm;
  min-height: 297mm;
  pointer-events: none;
}
</style>
