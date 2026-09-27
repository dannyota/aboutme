<script setup lang="ts">
import { computed } from 'vue';

import { useResizablePanel } from '../../composables/useResizablePanel';
import { editorShellCopy } from '../../i18n/editor-shell';

const { locale } = useLocale();
const copy = computed(() => editorShellCopy[locale.value]);
const panel = useResizablePanel();

// Dragging moves the panel's left edge with the pointer; matches the
// CropEditor.vue drag pattern (a single in-scope `drag` sentinel, optional
// pointer-capture calls so a DOM without them still works in tests).
let drag: { startClientX: number; startWidthRem: number } | null = null;
let previousBodyUserSelect = '';
let previousBodyCursor = '';

function beginDragStyles(): void {
  previousBodyUserSelect = document.body.style.userSelect;
  previousBodyCursor = document.body.style.cursor;
  // A fast drag must not select the rich text or labels it passes over.
  document.body.style.userSelect = 'none';
  document.body.style.cursor = 'col-resize';
}

function endDragStyles(): void {
  document.body.style.userSelect = previousBodyUserSelect;
  document.body.style.cursor = previousBodyCursor;
}

function onPointerDown(event: PointerEvent): void {
  drag = { startClientX: event.clientX, startWidthRem: panel.widthRem.value };
  (event.currentTarget as HTMLElement).setPointerCapture?.(event.pointerId);
  beginDragStyles();
  event.preventDefault();
}

function onPointerMove(event: PointerEvent): void {
  if (drag === null) return;
  // Moving left grows the panel; see useResizablePanel's handleKeyDown.
  const deltaPx = drag.startClientX - event.clientX;
  panel.setWidthRem(drag.startWidthRem + panel.pxToRem(deltaPx));
}

function onPointerUp(event: PointerEvent): void {
  if (drag === null) return;
  drag = null;
  const target = event.currentTarget as HTMLElement;
  if (target.hasPointerCapture?.(event.pointerId)) {
    target.releasePointerCapture(event.pointerId);
  }
  endDragStyles();
}
</script>

<template>
  <div
    aria-orientation="vertical"
    :aria-label="copy.resizePanel"
    :aria-valuemax="panel.maxRem.value"
    :aria-valuemin="panel.minRem"
    :aria-valuenow="panel.widthRem.value"
    :aria-valuetext="`${panel.widthRem.value}rem`"
    class="editor-panel-resize-handle group relative col-start-4
      row-start-2 w-2 shrink-0 cursor-col-resize touch-none
      justify-self-start select-none max-[72rem]:hidden"
    data-testid="panel-resize-handle"
    role="separator"
    tabindex="0"
    @dblclick="panel.reset()"
    @keydown="panel.handleKeyDown"
    @pointercancel="onPointerUp"
    @pointerdown="onPointerDown"
    @pointermove="onPointerMove"
    @pointerup="onPointerUp"
  >
    <span
      aria-hidden="true"
      class="absolute inset-y-0 left-0 w-px bg-border
        group-hover:bg-primary group-active:bg-primary
        group-focus-visible:bg-primary"
    />
  </div>
</template>
