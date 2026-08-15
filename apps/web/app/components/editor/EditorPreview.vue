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
import ResumeDocument from '../resume/ResumeDocument.vue';

const props = defineProps<{
  readonly document: Resume;
  readonly lng: string;
  readonly photoUrl?: string;
}>();

const previewRoot = shallowRef<HTMLElement | null>(null);
const estimatedPages = ref<number | null>(null);
const renderFailed = ref(false);
const requiresPhoto = computed(
  () => props.document.personalDetails.photo !== undefined,
);
const canRender = computed(
  () => !requiresPhoto.value || props.photoUrl !== undefined,
);
const context = computed(() => ({
  lng: props.lng,
  mode: 'paged' as const,
  ...(props.photoUrl === undefined ? {} : { photoUrl: props.photoUrl }),
}));
let stopObserving: (() => void) | undefined;

onErrorCaptured(() => {
  renderFailed.value = true;
  return false;
});

onMounted(async () => {
  await nextTick();
  if (previewRoot.value !== null) {
    stopObserving = observeSettledVisiblePageCount(
      previewRoot.value,
      (count) => {
        estimatedPages.value = count;
      },
    );
  }
});

onBeforeUnmount(() => stopObserving?.());
</script>

<template>
  <section
    class="editor-preview"
    aria-labelledby="editor-preview-title"
  >
    <header class="editor-preview__header">
      <h2 id="editor-preview-title">
        Preview
      </h2>
      <p class="editor-preview__count">
        <span data-estimated-pages-label>Estimated pages</span>
        <output aria-label="Estimated page count">
          {{ estimatedPages ?? "—" }}
        </output>
      </p>
    </header>
    <div
      ref="previewRoot"
      class="editor-preview__canvas"
    >
      <p
        v-if="!canRender"
        class="editor-preview__notice"
        role="status"
      >
        Preview is waiting for the authorized photo.
      </p>
      <p
        v-else-if="renderFailed"
        class="editor-preview__notice"
        role="status"
      >
        Preview is temporarily unavailable. Your edits are still safe.
      </p>
      <div
        v-else
        class="editor-preview__document"
      >
        <ResumeDocument
          :context="context"
          :document="document"
        />
      </div>
    </div>
  </section>
</template>
