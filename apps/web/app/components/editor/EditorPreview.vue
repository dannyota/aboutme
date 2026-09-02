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
import { previewProjection } from './previewProjection';

const props = defineProps<{
  readonly document: Resume;
  readonly lng: string;
  readonly zoom: 'fit' | 'full';
  readonly photoUrl?: string;
  readonly photoRead?: PhotoReadState;
}>();
const emit = defineEmits<{
  pages: [count: number];
}>();

const previewRoot = shallowRef<HTMLElement | null>(null);
const estimatedPages = ref<number | null>(null);
const renderFailed = ref(false);
const projected = computed(() =>
  previewProjection(props.document, props.photoUrl),
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
        emit('pages', count);
      },
    );
  }
});

onBeforeUnmount(() => stopObserving?.());
</script>

<template>
  <section
    class="grid h-full min-h-0 grid-rows-[minmax(0,1fr)]"
    aria-labelledby="editor-preview-title"
  >
    <div
      ref="previewRoot"
      class="overflow-auto bg-muted p-6"
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
        :class="[
          'mx-auto w-fit shadow-md',
          zoom === 'fit' ? '[zoom:0.84] max-[72rem]:[zoom:0.72]' : '[zoom:1]',
        ]"
      >
        <ResumeDocument
          :context="context"
          :document="projected"
        />
      </div>
    </div>
  </section>
</template>
