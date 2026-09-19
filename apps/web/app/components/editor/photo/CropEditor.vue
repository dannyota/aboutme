<script setup lang="ts">
/**
 * `CropEditor` — choose the square of the photo the resume shows.
 *
 * The stage draws the whole photo with the crop square over it: drag the
 * square (or click where it should go) to move it, use the slider to zoom,
 * or focus the stage and use the arrow keys and + / −. A small preview draws
 * the crop the way the renderer does. Exact 0–1 values stay available for
 * keyboard and precise entry. A photo with no crop starts on the default
 * square; a photo uploaded in this session saves that default at once.
 */
import type { PhotoCrop } from '@aboutme/schema';
import { computed, reactive, ref, useId, watch } from 'vue';
import FormField from '@/components/app/FormField.vue';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Slider } from '@/components/ui/slider';

import type { ResumeEditorActions } from '../../../composables/useResumeEditor';
import {
  cropImageStyle,
  defaultCrop,
  type ImageSize,
  isValidCrop,
  MAX_ZOOM,
  MIN_ZOOM,
  movedBy,
  withZoom,
  zoomOf,
} from '../../../editor/photoCrop';

const props = defineProps<{
  readonly actions: ResumeEditorActions;
  readonly crop?: PhotoCrop;
  readonly photoKey: string;
  readonly photoUrl: string;
  /** Save the default crop once this photo's size is known. */
  readonly saveDefault?: boolean;
}>();

type Field = 'x' | 'y' | 'width' | 'height';
const fields: readonly { key: Field; label: string }[] = [
  { key: 'x', label: 'X' },
  { key: 'y', label: 'Y' },
  { key: 'width', label: 'Width' },
  { key: 'height', label: 'Height' },
];

const WHOLE: PhotoCrop = { x: 0, y: 0, width: 1, height: 1 };
const hintId = `crop-hint-${useId()}`;
const zoomId = `crop-zoom-${useId()}`;

const image = ref<ImageSize | null>(null);
const draft = ref<PhotoCrop>(props.crop ?? WHOLE);
const exact = reactive<Record<Field, string>>({
  x: '0',
  y: '0',
  width: '1',
  height: '1',
});
const error = ref('');
const savedDefaultFor = new Set<string>();

const zoom = computed(() =>
  image.value === null ? MIN_ZOOM : zoomOf(image.value, draft.value));
const stageStyle = computed(() => ({
  aspectRatio: image.value === null
    ? '1 / 1'
    : `${image.value.width} / ${image.value.height}`,
}));
const squareStyle = computed(() => ({
  left: `${draft.value.x * 100}%`,
  top: `${draft.value.y * 100}%`,
  width: `${draft.value.width * 100}%`,
  height: `${draft.value.height * 100}%`,
}));
const previewStyle = computed(() => cropImageStyle(draft.value));
const changed = computed(() => !sameCrop(draft.value, props.crop));

watch(
  () => [props.photoKey, props.crop] as const,
  ([key], previous) => {
    if (previous !== undefined && previous[0] !== key) image.value = null;
    resetDraft();
  },
);
watch(draft, (crop) => {
  exact.x = String(crop.x);
  exact.y = String(crop.y);
  exact.width = String(crop.width);
  exact.height = String(crop.height);
}, { immediate: true });

function resetDraft(): void {
  error.value = '';
  draft.value = props.crop
    ?? (image.value === null ? WHOLE : defaultCrop(image.value));
}

function onImageLoad(event: Event): void {
  const img = event.target as HTMLImageElement;
  if (img.naturalWidth <= 0 || img.naturalHeight <= 0) return;
  image.value = { width: img.naturalWidth, height: img.naturalHeight };
  if (props.crop !== undefined) return;
  draft.value = defaultCrop(image.value);
  if (props.saveDefault && !savedDefaultFor.has(props.photoKey)) {
    savedDefaultFor.add(props.photoKey);
    props.actions.edit({ kind: 'photoCrop', crop: { ...draft.value } });
  }
}

function setZoom(value: number[] | undefined): void {
  const next = value?.[0];
  if (image.value === null || next === undefined) return;
  draft.value = withZoom(image.value, draft.value, next);
}

// Dragging moves the square with the pointer; pressing outside it first
// centres it on the pointer, so a click also places it.
let drag: { x: number; y: number; crop: PhotoCrop } | null = null;

function stagePoint(event: PointerEvent): { x: number; y: number } | null {
  const stage = event.currentTarget;
  if (!(stage instanceof HTMLElement)) return null;
  const rect = stage.getBoundingClientRect();
  if (rect.width <= 0 || rect.height <= 0) return null;
  return {
    x: (event.clientX - rect.left) / rect.width,
    y: (event.clientY - rect.top) / rect.height,
  };
}

function startDrag(event: PointerEvent): void {
  const point = stagePoint(event);
  if (point === null) return;
  const crop = draft.value;
  const inside = point.x >= crop.x && point.x <= crop.x + crop.width
    && point.y >= crop.y && point.y <= crop.y + crop.height;
  if (!inside) {
    draft.value = movedBy(
      crop,
      point.x - (crop.x + crop.width / 2),
      point.y - (crop.y + crop.height / 2),
    );
  }
  (event.currentTarget as HTMLElement).setPointerCapture?.(event.pointerId);
  drag = { ...point, crop: draft.value };
}

function moveDrag(event: PointerEvent): void {
  if (drag === null) return;
  const point = stagePoint(event);
  if (point === null) return;
  draft.value = movedBy(drag.crop, point.x - drag.x, point.y - drag.y);
}

function endDrag(event: PointerEvent): void {
  const stage = event.currentTarget as HTMLElement;
  if (stage.hasPointerCapture?.(event.pointerId)) {
    stage.releasePointerCapture(event.pointerId);
  }
  drag = null;
}

function onKeydown(event: KeyboardEvent): void {
  const step = event.shiftKey ? 0.1 : 0.02;
  const moves: Record<string, [number, number]> = {
    ArrowLeft: [-step, 0],
    ArrowRight: [step, 0],
    ArrowUp: [0, -step],
    ArrowDown: [0, step],
  };
  const move = moves[event.key];
  if (move !== undefined) {
    event.preventDefault();
    draft.value = movedBy(draft.value, move[0], move[1]);
    return;
  }
  if (event.key === '+' || event.key === '=' || event.key === '-') {
    event.preventDefault();
    setZoom([zoom.value + (event.key === '-' ? -0.25 : 0.25)]);
  }
}

function applyExact(): boolean {
  const crop = {
    x: Number(exact.x),
    y: Number(exact.y),
    width: Number(exact.width),
    height: Number(exact.height),
  };
  const blank = fields.some(({ key }) => String(exact[key]).trim() === '');
  if (blank || !isValidCrop(crop)) {
    error.value = 'Enter a crop within the image bounds.';
    return false;
  }
  error.value = '';
  draft.value = crop;
  return true;
}

function save(): void {
  if (!applyExact()) return;
  if (!changed.value) return;
  props.actions.edit({ kind: 'photoCrop', crop: { ...draft.value } });
}

function clearCrop(): void {
  error.value = '';
  props.actions.edit({ kind: 'photoCrop', crop: null });
}

function sameCrop(left: PhotoCrop, right: PhotoCrop | undefined): boolean {
  return right !== undefined
    && left.x === right.x && left.y === right.y
    && left.width === right.width && left.height === right.height;
}
</script>

<template>
  <form
    class="grid gap-4"
    novalidate
    @submit.prevent="save"
  >
    <fieldset class="grid min-w-0 gap-4">
      <legend class="mb-2 text-sm font-medium">
        Crop photo
      </legend>
      <p
        :id="hintId"
        class="text-sm text-muted-foreground"
      >
        Drag the square to choose what your resume shows. You can also focus
        the photo and use the arrow keys, and + or − to zoom.
      </p>
      <div class="flex flex-wrap items-start gap-4">
        <div
          aria-label="Crop position"
          :aria-describedby="hintId"
          class="relative w-full max-w-64 touch-none overflow-hidden rounded-md
            border bg-muted select-none focus-visible:ring-2
            focus-visible:ring-ring focus-visible:outline-none"
          data-crop-stage
          role="group"
          :style="stageStyle"
          tabindex="0"
          @keydown="onKeydown"
          @pointercancel="endDrag"
          @pointerdown="startDrag"
          @pointermove="moveDrag"
          @pointerup="endDrag"
        >
          <img
            alt=""
            class="pointer-events-none block size-full"
            draggable="false"
            :src="photoUrl"
            @load="onImageLoad"
          >
          <div
            aria-hidden="true"
            class="absolute cursor-move border-2 border-white
              shadow-[0_0_0_9999px_rgb(0_0_0/0.45)]"
            data-crop-rectangle
            :style="squareStyle"
          />
        </div>
        <figure class="grid justify-items-center gap-1">
          <div
            class="relative size-24 overflow-hidden border bg-muted
              rounded-[var(--radius-sheet)]"
            data-crop-preview
          >
            <img
              alt=""
              draggable="false"
              :src="photoUrl"
              :style="previewStyle"
            >
          </div>
          <figcaption class="text-xs text-muted-foreground">
            On your resume
          </figcaption>
        </figure>
      </div>
      <div class="grid max-w-64 gap-2">
        <Label :for="zoomId">Zoom</Label>
        <Slider
          :id="zoomId"
          :disabled="image === null"
          :max="MAX_ZOOM"
          :min="MIN_ZOOM"
          :model-value="[zoom]"
          :step="0.05"
          data-crop-zoom
          @update:model-value="setZoom"
        />
      </div>
      <details class="text-sm">
        <summary class="cursor-pointer text-muted-foreground">
          Exact values
        </summary>
        <div class="mt-3 grid grid-cols-2 gap-2">
          <FormField
            v-for="field in fields"
            :key="field.key"
            :label="field.label"
            :name="field.key"
          >
            <template #default="{ id, describedBy, invalid }">
              <Input
                :id="id"
                v-model="exact[field.key]"
                :aria-describedby="describedBy"
                :aria-invalid="invalid"
                max="1"
                min="0"
                :name="field.key"
                step="0.01"
                type="number"
                @change="applyExact"
              />
            </template>
          </FormField>
        </div>
      </details>
    </fieldset>
    <p
      v-if="error !== ''"
      class="text-sm text-destructive"
      role="alert"
    >
      {{ error }}
    </p>
    <div class="flex flex-wrap gap-2">
      <Button
        size="sm"
        type="submit"
      >
        Save crop
      </Button>
      <Button
        data-action="clear-crop"
        size="sm"
        type="button"
        variant="ghost"
        @click="clearCrop"
      >
        Clear crop
      </Button>
    </div>
  </form>
</template>
