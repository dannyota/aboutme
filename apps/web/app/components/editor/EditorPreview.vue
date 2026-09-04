<script setup lang="ts">
import type { Resume } from '@aboutme/schema';
import {
  computed,
  nextTick,
  onBeforeUnmount,
  onErrorCaptured,
  onMounted,
  ref,
  shallowRef,
} from 'vue';

import { observeSettledVisiblePageCount } from '../../editor/pageCountObserver';
import type { StampState } from '../../composables/useStamp';
import type { PhotoReadState } from '../../stores/resumes';
import AppSeal from '../app/AppSeal.vue';
import ResumeDocument from '../resume/ResumeDocument.vue';
import { previewProjection } from './previewProjection';

const props = withDefaults(defineProps<{
  readonly document: Resume;
  readonly lng: string;
  readonly zoom?: 'fit' | 'full';
  readonly photoUrl?: string;
  readonly photoRead?: PhotoReadState;
  readonly publicLink?: string | null;
  readonly stampState?: StampState;
}>(), { zoom: 'fit' });
const emit = defineEmits<{
  pages: [count: number];
}>();

const previewRoot = shallowRef<HTMLElement | null>(null);
const estimatedPages = ref<number | null>(null);
const renderFailed = ref(false);
const viewportWidth = ref<number | null>(null);
const windowWidth = ref<number | null>(null);
const projected = computed(() =>
  previewProjection(props.document, props.photoUrl),
);
const context = computed(() => ({
  lng: props.lng,
  mode: 'paged' as const,
  ...(props.photoUrl === undefined ? {} : { photoUrl: props.photoUrl }),
}));
let stopObserving: (() => void) | undefined;
let resizeObserver: ResizeObserver | undefined;

const A4_WIDTH_PX = 210 / 25.4 * 96;
const PHONE_BREAKPOINT_PX = 42 * 16;
const NARROW_BREAKPOINT_PX = 72 * 16;
const sheetZoom = computed(() => {
  const breakpointWidth = windowWidth.value;
  if (breakpointWidth !== null && breakpointWidth <= PHONE_BREAKPOINT_PX) {
    const availableWidth = viewportWidth.value ?? breakpointWidth;
    return Math.min(1, Math.max(0, availableWidth - 32) / A4_WIDTH_PX);
  }
  if (props.zoom === 'full') return 1;
  return breakpointWidth !== null && breakpointWidth <= NARROW_BREAKPOINT_PX
    ? 0.72
    : 0.84;
});
const scaledWidth = computed(() => A4_WIDTH_PX * sheetZoom.value);
const pageCountText = computed(() => {
  if (estimatedPages.value === 1) return '1 page';
  return `${estimatedPages.value ?? '—'} pages`;
});
const photoStatus = computed(() => {
  if (props.photoRead?.kind === 'loading') {
    return 'Photo is loading. The preview is shown without it.';
  }
  if (props.photoRead?.kind === 'suspended') {
    return 'Photo unavailable. The preview is shown without it.';
  }
  return '';
});
const updateWindowWidth = (): void => {
  windowWidth.value = window.innerWidth;
};

onErrorCaptured(() => {
  renderFailed.value = true;
  return false;
});

onMounted(async () => {
  await nextTick();
  updateWindowWidth();
  window.addEventListener('resize', updateWindowWidth);
  if (previewRoot.value !== null) {
    stopObserving = observeSettledVisiblePageCount(
      previewRoot.value,
      (count) => {
        estimatedPages.value = count;
        emit('pages', count);
      },
    );
    viewportWidth.value = previewRoot.value.clientWidth || window.innerWidth;
    if (typeof ResizeObserver !== 'undefined') {
      resizeObserver = new ResizeObserver(([entry]) => {
        if (entry !== undefined) viewportWidth.value = entry.contentRect.width;
      });
      resizeObserver.observe(previewRoot.value);
    }
  }
});

onBeforeUnmount(() => {
  stopObserving?.();
  resizeObserver?.disconnect();
  window.removeEventListener('resize', updateWindowWidth);
});
</script>

<template>
  <section
    aria-label="Resume preview"
    class="grid h-full min-h-0 grid-rows-[minmax(0,1fr)]"
  >
    <div
      ref="previewRoot"
      class="overflow-auto bg-background p-6 max-[42rem]:p-4"
      tabindex="0"
    >
      <p
        v-if="renderFailed"
        class="mx-auto mt-16 max-w-md rounded-lg border bg-card p-4
          text-muted-foreground"
        role="status"
      >
        Preview is temporarily unavailable. Your edits are still safe.
      </p>
      <div
        v-else
        class="mx-auto w-fit"
      >
        <div
          class="relative rounded-[var(--radius-sheet)] bg-white
            shadow-[var(--shadow-paper)]"
          :data-scaled-width="scaledWidth.toFixed(2)"
          :data-sheet-zoom="sheetZoom.toFixed(4)"
          data-testid="preview-sheet"
          :style="{ zoom: sheetZoom }"
        >
          <ResumeDocument
            :context="context"
            :document="projected"
          />
          <AppSeal
            v-if="publicLink"
            class="pointer-events-none absolute right-5 bottom-5"
            :data-stamp="stampState === 'idle' ? undefined : stampState"
            data-testid="preview-stamp"
            :link="publicLink"
            size="stamp"
          />
        </div>
        <div class="mt-3 flex flex-wrap items-center gap-3">
          <p
            class="inline-flex items-center gap-1.5 text-sm
              text-muted-foreground"
            data-testid="page-count"
          >
            <svg
              aria-hidden="true"
              class="size-3.5"
              data-page-count-glyph
              fill="none"
              viewBox="0 0 14 14"
            >
              <path
                d="m2 10.75 1.1-3.3L9.8.75l2.45 2.45-6.7 6.7L2 10.75Z"
                stroke="currentColor"
                stroke-linejoin="round"
                stroke-width="1.25"
              />
            </svg>
            {{ pageCountText }}
          </p>
          <p
            v-if="photoStatus !== ''"
            class="text-sm text-muted-foreground"
            data-photo-state
            role="status"
          >
            {{ photoStatus }}
          </p>
        </div>
      </div>
    </div>
  </section>
</template>
