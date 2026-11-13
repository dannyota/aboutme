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
  watch,
} from 'vue';

import { observeSettledVisiblePageCount } from '../../editor/pageCountObserver';
import {
  type PreviewZoomStep,
  previewZoomPercent,
  readStoredPreviewZoom,
  stepPreviewZoom,
  writeStoredPreviewZoom,
} from '../../editor/previewZoom';
import type { PdfDownloadController } from '../../editor/pdfDownload';
import type { PhotoReadState } from '../../stores/resumes';
import { Button } from '../ui/button';
import PDFOpenButton from './PDFOpenButton.vue';
import PreviewZoomControls from './PreviewZoomControls.vue';
import ResumeDocument from '../resume/ResumeDocument.vue';
import ScaledSheet from '../resume/ScaledSheet.vue';
import { previewProjection } from './previewProjection';
import { editorShellCopy } from '../../i18n/editor-shell';

type PreviewMode = 'pdf' | 'web';
const PREVIEW_MODE_STORAGE_KEY = 'aboutme.editorPreviewMode';

function readStoredPreviewMode(): PreviewMode {
  try {
    return window.localStorage.getItem(PREVIEW_MODE_STORAGE_KEY) === 'web'
      ? 'web'
      : 'pdf';
  } catch {
    return 'pdf';
  }
}

function writeStoredPreviewMode(mode: PreviewMode): void {
  try {
    window.localStorage.setItem(PREVIEW_MODE_STORAGE_KEY, mode);
  } catch {
    // A blocked or full store keeps today's in-memory choice for this tab.
  }
}

const props = withDefaults(defineProps<{
  readonly document: Resume;
  readonly lng: string;
  readonly zoom?: 'fit' | 'full';
  readonly photoUrl?: string;
  readonly photoRead?: PhotoReadState;
  readonly active?: boolean;
  readonly pdfController?: PdfDownloadController;
}>(), { zoom: 'fit', active: true });
const emit = defineEmits<{
  pages: [count: number];
}>();
const { locale } = useLocale();
const copy = computed(() => editorShellCopy[locale.value]);

const previewRoot = shallowRef<HTMLElement | null>(null);
const previewMode = ref<PreviewMode>(readStoredPreviewMode());
const estimatedPages = ref<number | null>(null);
const renderFailed = ref(false);
const viewportWidth = ref<number | null>(null);
const windowWidth = ref<number | null>(null);
const projected = computed(() =>
  previewProjection(props.document, props.photoUrl),
);
const colorScheme = computed<'dark' | 'system' | undefined>(() => {
  if (previewMode.value !== 'web') return undefined;
  const scheme = props.document.customization.colorScheme;
  return scheme === 'dark' || scheme === 'system' ? scheme : undefined;
});
const context = computed(() => ({
  lng: props.lng,
  mode: previewMode.value === 'web'
    ? 'continuous' as const
    : 'paged' as const,
  nameHeading: 'p' as const,
  ...(props.photoUrl === undefined ? {} : { photoUrl: props.photoUrl }),
  ...(colorScheme.value === undefined
    ? {}
    : { colorScheme: colorScheme.value }),
}));
let stopObserving: (() => void) | undefined;
let resizeObserver: ResizeObserver | undefined;

const A4_WIDTH_PX = 210 / 25.4 * 96;
const PHONE_BREAKPOINT_PX = 42 * 16;
const NARROW_BREAKPOINT_PX = 72 * 16;

const currentZoom = ref<PreviewZoomStep>(props.zoom === 'full' ? 100 : 'fit');
const isPhoneLayout = computed(() => (
  windowWidth.value !== null && windowWidth.value <= PHONE_BREAKPOINT_PX
));
const fitPercent = computed(() => (
  windowWidth.value !== null && windowWidth.value <= NARROW_BREAKPOINT_PX
    ? 72
    : 84
));
const zoomPercent = computed(() => (
  previewZoomPercent(currentZoom.value, fitPercent.value)
));
const isFit = computed(() => currentZoom.value === 'fit');
const showZoomControls = computed(() => (
  previewMode.value === 'pdf' && !isPhoneLayout.value
));
const zoomOutDisabled = computed(() => currentZoom.value === 50);
const zoomInDisabled = computed(() => currentZoom.value === 200);
const zoomAnnouncement = ref('');

const sheetZoom = computed(() => {
  const breakpointWidth = windowWidth.value;
  if (breakpointWidth !== null && breakpointWidth <= PHONE_BREAKPOINT_PX) {
    const availableWidth = viewportWidth.value ?? breakpointWidth;
    return Math.min(1, Math.max(0, availableWidth - 32) / A4_WIDTH_PX);
  }
  return zoomPercent.value / 100;
});
const scaledWidth = computed(() => A4_WIDTH_PX * sheetZoom.value);
const pageCountText = computed(() => {
  return copy.value.pageCount(estimatedPages.value);
});
const photoStatus = computed(() => {
  if (props.photoRead?.kind === 'loading') {
    return copy.value.photoLoading;
  }
  if (props.photoRead?.kind === 'suspended') {
    return copy.value.photoUnavailable;
  }
  return '';
});
const updateWindowWidth = (): void => {
  windowWidth.value = window.innerWidth;
};

function applyZoom(
  next: PreviewZoomStep,
  pivotClient?: { x: number; y: number },
): void {
  const changed = next !== currentZoom.value;
  const root = previewRoot.value;
  if (root === null) {
    currentZoom.value = next;
    if (changed) announceZoom(next);
    return;
  }
  const rect = root.getBoundingClientRect();
  const pivotX = pivotClient?.x ?? rect.left + root.clientWidth / 2;
  const pivotY = pivotClient?.y ?? rect.top + root.clientHeight / 2;
  const offsetX = pivotX - rect.left;
  const offsetY = pivotY - rect.top;
  const beforeWidth = root.scrollWidth || 1;
  const beforeHeight = root.scrollHeight || 1;
  const ratioX = (root.scrollLeft + offsetX) / beforeWidth;
  const ratioY = (root.scrollTop + offsetY) / beforeHeight;

  currentZoom.value = next;
  if (changed) announceZoom(next);

  void nextTick(() => {
    const afterRoot = previewRoot.value;
    if (afterRoot === null) return;
    afterRoot.scrollLeft = ratioX * afterRoot.scrollWidth - offsetX;
    afterRoot.scrollTop = ratioY * afterRoot.scrollHeight - offsetY;
  });
}

function announceZoom(step: PreviewZoomStep): void {
  zoomAnnouncement.value = copy.value.zoomAnnounce(
    previewZoomPercent(step, fitPercent.value),
  );
}

function onPreviewKeydown(event: KeyboardEvent): void {
  if (
    previewMode.value !== 'pdf'
    || isPhoneLayout.value
    || !(event.ctrlKey || event.metaKey)
  ) return;
  if (event.key === '0') {
    event.preventDefault();
    applyZoom('fit');
  } else if (event.key === '+' || event.key === '=') {
    event.preventDefault();
    applyZoom(stepPreviewZoom(currentZoom.value, 1, fitPercent.value));
  } else if (event.key === '-' || event.key === '_') {
    event.preventDefault();
    applyZoom(stepPreviewZoom(currentZoom.value, -1, fitPercent.value));
  }
}

// Chromium and Firefox report a trackpad pinch as Ctrl + wheel events.
const WHEEL_ZOOM_STEP_PX = 50;
const WHEEL_ZOOM_IDLE_MS = 300;
let wheelAccumulatedPx = 0;
let wheelDirection: 1 | -1 | null = null;
let wheelIdleTimer: ReturnType<typeof setTimeout> | undefined;

function resetWheelAccumulator(): void {
  wheelAccumulatedPx = 0;
  wheelDirection = null;
}

function onPreviewWheel(event: WheelEvent): void {
  if (
    previewMode.value !== 'pdf'
    || isPhoneLayout.value
    || !(event.ctrlKey || event.metaKey)
  ) return;
  event.preventDefault();
  const direction: 1 | -1 = event.deltaY < 0 ? 1 : -1;
  if (direction !== wheelDirection) {
    wheelAccumulatedPx = 0;
    wheelDirection = direction;
  }
  clearTimeout(wheelIdleTimer);
  wheelIdleTimer = setTimeout(resetWheelAccumulator, WHEEL_ZOOM_IDLE_MS);
  wheelAccumulatedPx += Math.abs(event.deltaY);

  if (wheelAccumulatedPx < WHEEL_ZOOM_STEP_PX) return;
  wheelAccumulatedPx = 0;
  const next = stepPreviewZoom(currentZoom.value, direction, fitPercent.value);
  if (next === currentZoom.value) return;
  applyZoom(next, { x: event.clientX, y: event.clientY });
}

function startPageCountObservation(): void {
  stopObserving?.();
  if (previewRoot.value === null) return;
  stopObserving = observeSettledVisiblePageCount(
    previewRoot.value,
    (count) => {
      estimatedPages.value = count;
      emit('pages', count);
    },
  );
}

watch(previewMode, (mode) => {
  writeStoredPreviewMode(mode);
  if (mode === 'web') estimatedPages.value = null;
});

watch(currentZoom, (step) => writeStoredPreviewZoom(step));

onErrorCaptured(() => {
  renderFailed.value = true;
  return false;
});

onMounted(async () => {
  const stored = readStoredPreviewZoom();
  if (stored !== undefined) currentZoom.value = stored;
  await nextTick();
  updateWindowWidth();
  window.addEventListener('resize', updateWindowWidth);
  if (previewRoot.value !== null) {
    startPageCountObservation();
    viewportWidth.value = previewRoot.value.clientWidth || window.innerWidth;
    if (typeof ResizeObserver !== 'undefined') {
      resizeObserver = new ResizeObserver(([entry]) => {
        if (entry !== undefined) viewportWidth.value = entry.contentRect.width;
      });
      resizeObserver.observe(previewRoot.value);
    }
  }
});

watch(() => props.active, async (active) => {
  if (!active) return;
  await nextTick();
  startPageCountObservation();
});

onBeforeUnmount(() => {
  stopObserving?.();
  resizeObserver?.disconnect();
  window.removeEventListener('resize', updateWindowWidth);
  clearTimeout(wheelIdleTimer);
});
</script>

<template>
  <section
    :aria-label="copy.previewLabel"
    class="relative grid h-full min-h-0 grid-rows-[auto_minmax(0,1fr)]"
    @keydown="onPreviewKeydown"
  >
    <div
      class="flex items-center justify-end border-b border-border bg-card
        px-4 py-2 max-[42rem]:px-2"
      data-testid="preview-toolbar"
    >
      <div
        :aria-label="copy.previewMode"
        class="flex w-fit rounded-md border bg-background p-0.5"
        data-testid="preview-mode-switch"
        role="group"
      >
        <Button
          :aria-pressed="previewMode === 'pdf'"
          class="h-8"
          data-mode="pdf"
          size="sm"
          type="button"
          :variant="previewMode === 'pdf' ? 'default' : 'ghost'"
          @click="previewMode = 'pdf'"
        >
          {{ copy.previewModePdf }}
        </Button>
        <Button
          :aria-pressed="previewMode === 'web'"
          class="h-8"
          data-mode="web"
          size="sm"
          type="button"
          :variant="previewMode === 'web' ? 'default' : 'ghost'"
          @click="previewMode = 'web'"
        >
          {{ copy.previewModeWeb }}
        </Button>
      </div>
    </div>
    <div
      ref="previewRoot"
      class="overflow-auto bg-editor-canvas p-6 max-[42rem]:p-4"
      :class="showZoomControls ? 'pb-18' : undefined"
      data-testid="preview-scroll"
      tabindex="0"
      @wheel="onPreviewWheel"
    >
      <p
        v-if="renderFailed"
        class="mx-auto mt-16 max-w-md rounded-lg border bg-card p-4
          text-muted-foreground"
        role="status"
      >
        {{ copy.previewUnavailable }}
      </p>
      <div
        v-else
        :class="previewMode === 'web' ? 'w-full' : 'mx-auto w-fit'"
      >
        <div
          class="preview-sheet rounded-[var(--radius-sheet)]
            shadow-[var(--shadow-paper)]"
          :class="[
            colorScheme === undefined ? 'bg-white' : undefined,
            previewMode === 'web' ? 'overflow-hidden' : undefined,
          ]"
          :data-color-scheme="colorScheme"
          :data-web-columns="previewMode === 'web'
            ? document.customization.layout.columns
            : undefined"
          :data-scaled-width="
            previewMode === 'pdf' ? scaledWidth.toFixed(2) : undefined
          "
          :data-sheet-zoom="
            previewMode === 'pdf' ? sheetZoom.toFixed(4) : undefined
          "
          data-testid="preview-sheet"
        >
          <ScaledSheet
            v-if="previewMode === 'pdf'"
            :scale="sheetZoom"
          >
            <ResumeDocument
              :context="context"
              :document="projected"
            />
          </ScaledSheet>
          <ResumeDocument
            v-else
            :context="context"
            :document="projected"
          />
        </div>
        <div class="mt-3 flex flex-wrap items-center gap-3">
          <p
            v-if="previewMode === 'pdf'"
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
          <PDFOpenButton
            v-if="previewMode === 'pdf' && pdfController !== undefined"
            :controller="pdfController"
          />
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
    <PreviewZoomControls
      v-if="showZoomControls"
      :announcement="zoomAnnouncement"
      class="absolute bottom-4 right-4 z-10"
      :fit-label="copy.zoomFit"
      :is-fit="isFit"
      :percent="zoomPercent"
      :percent-label="copy.zoomReset(zoomPercent)"
      :zoom-in-disabled="zoomInDisabled"
      :zoom-in-label="copy.zoomIn"
      :zoom-out-disabled="zoomOutDisabled"
      :zoom-out-label="copy.zoomOut"
      @set-fit="applyZoom('fit')"
      @zoom-in="applyZoom(stepPreviewZoom(currentZoom, 1, fitPercent))"
      @zoom-out="applyZoom(stepPreviewZoom(currentZoom, -1, fitPercent))"
    />
  </section>
</template>

<style>
.preview-sheet:has(.paged-resume) {
  background: transparent;
  box-shadow: none;
}

.preview-sheet .paged-resume > .resume-page {
  border-radius: var(--radius-sheet);
  box-shadow: var(--shadow-paper);
}
</style>
