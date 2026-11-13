<script setup lang="ts">
import type { TemplatePreset } from '@aboutme/schema/templates';
import { computed, onBeforeUnmount, onMounted, ref } from 'vue';

import ResumeDocument from '../../resume/ResumeDocument.vue';
import ScaledSheet from '../../resume/ScaledSheet.vue';
import { applyTemplate } from '../../resume/applyTemplate';
import { sampleContext } from '../../../landing/sampleContext';
import { sampleResume } from '../../../landing/sampleResume';

const SCALE = 0.18;

const props = defineProps<{ readonly preset: Readonly<TemplatePreset> }>();

const root = ref<HTMLElement>();
const visible = ref(false);
let observer: IntersectionObserver | undefined;

const document = computed(() => ({
  ...sampleResume,
  customization: applyTemplate(
    sampleResume.customization,
    props.preset,
    sampleResume.content,
    sampleContext.lng,
  ),
}));

onMounted(() => {
  if (root.value === undefined || typeof IntersectionObserver === 'undefined') {
    return;
  }
  observer = new IntersectionObserver(
    (entries) => {
      visible.value = entries.some((entry) => entry.isIntersecting);
    },
    { rootMargin: '200px 0px' },
  );
  observer.observe(root.value);
});

onBeforeUnmount(() => observer?.disconnect());
</script>

<template>
  <div
    ref="root"
    aria-hidden="true"
    class="template-thumbnail h-[202px] w-[143px] shrink-0 overflow-hidden
      rounded-[var(--radius-sheet)] bg-white shadow-[var(--shadow-paper)]"
    data-template-thumbnail
    inert
  >
    <ScaledSheet
      v-if="visible"
      :scale="SCALE"
    >
      <div
        class="template-thumbnail__page"
        data-template-thumbnail-render
      >
        <ResumeDocument
          :context="sampleContext"
          :document="document"
        />
      </div>
    </ScaledSheet>
  </div>
</template>

<style scoped>
.template-thumbnail__page {
  width: 210mm;
  min-height: 297mm;
  pointer-events: none;
}
</style>
