<script setup lang="ts">
import { Maximize, ZoomIn, ZoomOut } from '@lucide/vue';
import { Button } from '../ui/button';
import IconButton from '../app/IconButton.vue';

defineProps<{
  readonly percent: number;
  readonly isFit: boolean;
  readonly zoomOutDisabled: boolean;
  readonly zoomInDisabled: boolean;
  readonly zoomOutLabel: string;
  readonly zoomInLabel: string;
  readonly fitLabel: string;
  readonly percentLabel: string;
}>();

defineEmits<{
  zoomOut: [];
  zoomIn: [];
  setFit: [];
}>();
</script>

<template>
  <div
    class="flex items-center gap-0.5 rounded-md border bg-card p-1
      shadow-[var(--shadow-paper)]"
    data-testid="preview-zoom-controls"
  >
    <IconButton
      data-testid="zoom-out"
      :disabled="zoomOutDisabled"
      :label="zoomOutLabel"
      size="icon-sm"
      @click="$emit('zoomOut')"
    >
      <ZoomOut
        aria-hidden="true"
        class="size-4"
      />
    </IconButton>
    <Button
      :aria-label="percentLabel"
      :aria-pressed="isFit"
      class="h-8 min-w-14 px-2 text-xs tabular-nums"
      data-testid="zoom-percent"
      size="sm"
      type="button"
      variant="ghost"
      @click="$emit('setFit')"
    >
      {{ percent }}%
    </Button>
    <IconButton
      data-testid="zoom-in"
      :disabled="zoomInDisabled"
      :label="zoomInLabel"
      size="icon-sm"
      @click="$emit('zoomIn')"
    >
      <ZoomIn
        aria-hidden="true"
        class="size-4"
      />
    </IconButton>
    <IconButton
      data-testid="zoom-fit"
      :label="fitLabel"
      :pressed="isFit"
      size="icon-sm"
      :variant="isFit ? 'default' : 'ghost'"
      @click="$emit('setFit')"
    >
      <Maximize
        aria-hidden="true"
        class="size-4"
      />
    </IconButton>
  </div>
</template>
