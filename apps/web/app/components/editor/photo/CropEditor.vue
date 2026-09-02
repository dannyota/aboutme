<script setup lang="ts">
import type { PhotoCrop } from '@aboutme/schema';
import { computed, reactive, ref, watch } from 'vue';
import FormField from '@/components/app/FormField.vue';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';

import type { ResumeEditorActions } from '../../../composables/useResumeEditor';

const props = defineProps<{
  readonly actions: ResumeEditorActions;
  readonly crop?: PhotoCrop;
  readonly photoKey: string;
  readonly photoUrl: string;
}>();

type CropDraftValue = number | string;
type CropDraft = Record<'height' | 'width' | 'x' | 'y', CropDraftValue>;

const draft = reactive<CropDraft>({ x: '0', y: '0', width: '1', height: '1' });
const error = ref('');
const dragging = ref(false);
const rectangleStyle = computed(() => ({
  height: `${numberOr(draft.height, 1) * 100}%`,
  left: `${numberOr(draft.x, 0) * 100}%`,
  top: `${numberOr(draft.y, 0) * 100}%`,
  width: `${numberOr(draft.width, 1) * 100}%`,
}));

watch(
  () => [props.photoKey, props.crop] as const,
  () => {
    const crop = props.crop;
    draft.x = String(crop?.x ?? 0);
    draft.y = String(crop?.y ?? 0);
    draft.width = String(crop?.width ?? 1);
    draft.height = String(crop?.height ?? 1);
    error.value = '';
  },
  { immediate: true },
);

function submitCrop(): void {
  const crop = parseBoundedCrop(draft);
  if (crop === undefined) {
    error.value = 'Enter a crop within the image bounds.';
    return;
  }
  error.value = '';
  props.actions.edit({ kind: 'photoCrop', crop });
}

function clearCrop(): void {
  error.value = '';
  props.actions.edit({ kind: 'photoCrop', crop: null });
}

function startDrag(event: PointerEvent): void {
  const target = event.currentTarget;
  if (!(target instanceof HTMLElement)) return;
  target.setPointerCapture(event.pointerId);
  dragging.value = true;
  moveCrop(event);
}

function moveCrop(event: PointerEvent): void {
  const target = event.currentTarget;
  if (!(target instanceof HTMLElement)) return;
  const rect = target.getBoundingClientRect();
  if (rect.width <= 0 || rect.height <= 0) return;
  const width = numberOr(draft.width, 1);
  const height = numberOr(draft.height, 1);
  draft.x = String(roundCrop(
    clamp((event.clientX - rect.left) / rect.width, 0, 1 - width),
  ));
  draft.y = String(roundCrop(
    clamp((event.clientY - rect.top) / rect.height, 0, 1 - height),
  ));
}

function endDrag(event: PointerEvent): void {
  const target = event.currentTarget;
  if (
    target instanceof HTMLElement
    && target.hasPointerCapture(event.pointerId)
  ) {
    target.releasePointerCapture(event.pointerId);
  }
  dragging.value = false;
}

function dragCrop(event: PointerEvent): void {
  if (!dragging.value) return;
  moveCrop(event);
}

function moveWithKeyboard(event: KeyboardEvent): void {
  const step = event.shiftKey ? 0.1 : 0.05;
  const width = numberOr(draft.width, 1);
  const height = numberOr(draft.height, 1);
  if (event.key === 'ArrowLeft' || event.key === 'ArrowRight') {
    event.preventDefault();
    const offset = event.key === 'ArrowLeft' ? -step : step;
    draft.x = String(roundCrop(
      clamp(numberOr(draft.x, 0) + offset, 0, 1 - width),
    ));
  }
  if (event.key === 'ArrowUp' || event.key === 'ArrowDown') {
    event.preventDefault();
    const offset = event.key === 'ArrowUp' ? -step : step;
    draft.y = String(roundCrop(
      clamp(numberOr(draft.y, 0) + offset, 0, 1 - height),
    ));
  }
}

function parseBoundedCrop(value: CropDraft): PhotoCrop | undefined {
  if ([value.x, value.y, value.width, value.height].some(
    (part) => String(part).trim() === '',
  )) {
    return undefined;
  }
  const crop = {
    x: Number(value.x),
    y: Number(value.y),
    width: Number(value.width),
    height: Number(value.height),
  };
  if (!Object.values(crop).every(Number.isFinite)) return undefined;
  if (crop.x < 0 || crop.y < 0 || crop.width <= 0 || crop.height <= 0) {
    return undefined;
  }
  if (
    crop.width > 1
    || crop.height > 1
    || crop.x + crop.width > 1
    || crop.y + crop.height > 1
  ) {
    return undefined;
  }
  return crop;
}

function numberOr(value: CropDraftValue, fallback: number): number {
  const parsed = Number(value);
  return Number.isFinite(parsed) ? parsed : fallback;
}

function clamp(value: number, minimum: number, maximum: number): number {
  return Math.min(maximum, Math.max(minimum, value));
}

function roundCrop(value: number): number {
  return Math.round(value * 1_000_000) / 1_000_000;
}
</script>

<template>
  <form
    novalidate
    @submit.prevent="submitCrop"
  >
    <fieldset class="grid gap-4">
      <legend>Crop photo</legend>
      <div
        aria-label="Crop position"
        data-crop-stage
        class="relative aspect-square max-w-64 overflow-hidden rounded-md border
          bg-muted focus-visible:ring-2"
        role="application"
        tabindex="0"
        @keydown="moveWithKeyboard"
        @pointercancel="endDrag"
        @pointerdown="startDrag"
        @pointermove="dragCrop"
        @pointerup="endDrag"
      >
        <img
          :src="photoUrl"
          alt=""
        >
        <div
          aria-hidden="true"
          data-crop-rectangle
          class="absolute border-2 border-positive bg-positive/10"
          :style="rectangleStyle"
        />
      </div>
      <div class="grid grid-cols-2 gap-2">
        <FormField
          label="X"
          name="x"
        >
          <template #default="{ id, describedBy, invalid }">
            <Input
              :id="id"
              v-model="draft.x"
              name="x"
              type="number"
              :aria-describedby="describedBy"
              :aria-invalid="invalid"
              min="0"
              max="1"
              step="0.01"
            />
          </template>
        </FormField>
        <FormField
          label="Y"
          name="y"
        >
          <template #default="{ id, describedBy, invalid }">
            <Input
              :id="id"
              v-model="draft.y"
              name="y"
              type="number"
              :aria-describedby="describedBy"
              :aria-invalid="invalid"
              min="0"
              max="1"
              step="0.01"
            />
          </template>
        </FormField>
        <FormField
          label="Width"
          name="width"
        >
          <template #default="{ id, describedBy, invalid }">
            <Input
              :id="id"
              v-model="draft.width"
              name="width"
              type="number"
              :aria-describedby="describedBy"
              :aria-invalid="invalid"
              min="0"
              max="1"
              step="0.01"
            />
          </template>
        </FormField>
        <FormField
          label="Height"
          name="height"
        >
          <template #default="{ id, describedBy, invalid }">
            <Input
              :id="id"
              v-model="draft.height"
              name="height"
              type="number"
              :aria-describedby="describedBy"
              :aria-invalid="invalid"
              min="0"
              max="1"
              step="0.01"
            />
          </template>
        </FormField>
      </div>
    </fieldset>
    <p
      v-if="error !== ''"
      role="alert"
    >
      {{ error }}
    </p>
    <Button
      type="submit"
      size="sm"
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
  </form>
</template>
