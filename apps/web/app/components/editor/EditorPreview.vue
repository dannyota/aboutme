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
import type { PhotoReadState } from '../../stores/resumes';
import ResumeDocument from '../resume/ResumeDocument.vue';
import {
  photoStateFor,
  previewProjection,
} from './previewProjection';

const props = defineProps<{
  readonly document: Resume;
  readonly lng: string;
  readonly photoUrl?: string;
  readonly photoRead?: PhotoReadState;
}>();

const previewRoot = shallowRef<HTMLElement | null>(null);
const estimatedPages = ref<number | null>(null);
const renderFailed = ref(false);
const projected = computed(() =>
  previewProjection(props.document, props.photoUrl),
);
const photoState = computed(() =>
  photoStateFor(
    props.photoRead,
    props.document.personalDetails.photo !== undefined,
  ),
);
const photoNotice = computed(() => {
  switch (photoState.value) {
    case 'loading':
      return 'Photo is loading. The preview is shown without it.';
    case 'unavailable':
      return (
        'Photo unavailable. The preview is shown without it. '
        + 'Open the Photo panel to retry.'
      );
    default:
      return '';
  }
});
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
      tabindex="0"
    >
      <p
        v-if="photoNotice !== ''"
        class="editor-preview__notice"
        role="status"
      >
        {{ photoNotice }}
      </p>
      <p
        v-if="renderFailed"
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
          :document="projected"
        />
      </div>
    </div>
  </section>
</template>
