<script setup lang="ts">
/**
 * `TemplateThumbnail` — the compiled-in sample resume rendered through the
 * shared renderer with one template applied, at thumbnail scale.
 *
 * A render mounts only while its card is near the viewport and unmounts when
 * it scrolls away, so an open picker holds a few renders, not all twenty. The
 * picture is decorative: the card's name and description carry the meaning.
 */
import type { TemplatePreset } from '@aboutme/schema/templates';
import { computed, onBeforeUnmount, onMounted, ref } from 'vue';

import ResumeDocument from '../../resume/ResumeDocument.vue';
import { applyTemplate } from '../../resume/applyTemplate';
import { sampleContext } from '../../../landing/sampleContext';
import { sampleResume } from '../../../landing/sampleResume';

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
  ),
}));

onMounted(() => {
  // Without an observer (old browsers, tests) the card keeps its blank page.
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
    <div
      v-if="visible"
      class="template-thumbnail__page"
      data-template-thumbnail-render
    >
      <ResumeDocument
        :context="sampleContext"
        :document="document"
      />
    </div>
  </div>
</template>

<style scoped>
/* An A4 sheet at 0.18 is 143 × 202 CSS pixels, the box above. */
.template-thumbnail__page {
  width: 210mm;
  min-height: 297mm;
  zoom: 0.18;
  pointer-events: none;
}
</style>
