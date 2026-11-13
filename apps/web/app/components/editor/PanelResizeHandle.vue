<script setup lang="ts">
import { computed, onBeforeUnmount } from 'vue';

import { useResizablePanel } from '../../composables/useResizablePanel';
import { editorShellCopy } from '../../i18n/editor-shell';

const { locale } = useLocale();
const copy = computed(() => editorShellCopy[locale.value]);
const panel = useResizablePanel();

let drag: { startClientX: number; startWidthRem: number } | null = null;
let previousBodyUserSelect = '';
let previousBodyCursor = '';

function beginDragStyles(): void {
  previousBodyUserSelect = document.body.style.userSelect;
  previousBodyCursor = document.body.style.cursor;
  document.body.style.userSelect = 'none';
  document.body.style.cursor = 'col-resize';
}

function endDragStyles(): void {
  document.body.style.userSelect = previousBodyUserSelect;
  document.body.style.cursor = previousBodyCursor;
}

function onPointerDown(event: PointerEvent): void {
  if (event.button !== 0 || drag !== null) return;
  drag = { startClientX: event.clientX, startWidthRem: panel.widthRem.value };
  (event.currentTarget as HTMLElement).setPointerCapture?.(event.pointerId);
  beginDragStyles();
  event.preventDefault();
}

function onPointerMove(event: PointerEvent): void {
  if (drag === null) return;
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
  panel.commit();
}

function abandonDrag(): void {
  if (drag === null) return;
  drag = null;
  endDragStyles();
  panel.commit();
}

onBeforeUnmount(abandonDrag);

const announcedWidthRem = computed(() => Math.round(panel.widthRem.value));
</script>

<template>
  <div
    aria-orientation="vertical"
    :aria-label="copy.resizePanel"
    :aria-valuemax="Math.round(panel.maxRem.value)"
    :aria-valuemin="panel.minRem"
    :aria-valuenow="announcedWidthRem"
    :aria-valuetext="`${announcedWidthRem}rem`"
    class="editor-panel-resize-handle group relative z-10 col-start-4
      row-start-2 w-2 shrink-0 cursor-col-resize touch-none
      justify-self-start select-none max-[72rem]:hidden
      focus-visible:-outline-offset-2"
    data-testid="panel-resize-handle"
    role="separator"
    tabindex="0"
    @dblclick="panel.reset()"
    @keydown="panel.handleKeyDown"
    @lostpointercapture="abandonDrag"
    @pointercancel="onPointerUp"
    @pointerdown="onPointerDown"
    @pointermove="onPointerMove"
    @pointerup="onPointerUp"
  >
    <span
      aria-hidden="true"
      class="absolute inset-y-0 left-0 w-px bg-border
        group-hover:w-[3px] group-hover:bg-primary
        group-active:w-[3px] group-active:bg-primary
        group-focus-visible:w-[3px] group-focus-visible:bg-primary"
    />
  </div>
</template>
