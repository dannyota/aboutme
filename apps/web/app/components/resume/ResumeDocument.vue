<script setup lang="ts">
import type { Resume } from '@aboutme/schema';
import { computed, type CSSProperties } from 'vue';

import LayoutColumns from './LayoutColumns.vue';
import {
  type RenderContext,
  resolveRenderModel,
} from './resolveRenderModel';
import ResumeHeader from './ResumeHeader.vue';

const props = defineProps<{
  document: Resume;
  context: RenderContext;
}>();

const model = computed(() => resolveRenderModel(props.document, props.context));
const rootStyle = computed<CSSProperties>(() => ({
  ...model.value.styles.root,
  fontSynthesis: 'none',
  printColorAdjust: 'exact' as const,
  WebkitPrintColorAdjust: 'exact' as const,
}));
</script>

<template>
  <article
    class="resume-document"
    :lang="model.lng"
    :style="rootStyle"
  >
    <div :style="model.styles.header">
      <ResumeHeader
        :personal-details="model.personalDetails"
        :header="model.header"
        :photo="model.photo"
      />
    </div>
    <LayoutColumns :model="model" />
  </article>
</template>

<style scoped>
.resume-document {
  box-sizing: border-box;
  min-height: 100%;
  padding: var(--page-margin-y) var(--page-margin-x);
  color: var(--color-body);
  background: var(--color-surface);
  font-family: var(--font-family);
  font-size: var(--fs-body);
  line-height: var(--lh-body);
}

.resume-document :deep(*) {
  box-sizing: border-box;
}

.resume-document :deep(a) {
  color: var(--color-link);
  text-decoration: underline;
}

.resume-document :deep(.resume-header) {
  margin-block-end: var(--gap-section);
  break-inside: avoid;
}

.resume-document :deep(.resume-photo) {
  width: var(--photo-size);
  height: var(--photo-size);
  border-radius: var(--photo-radius);
}

.resume-document :deep(.resume-name) {
  margin: 0;
  color: var(--color-heading);
  font-size: var(--fs-name);
  line-height: var(--lh-heading);
}

.resume-document :deep(.resume-headline) {
  margin: 0;
  color: var(--color-body);
  font-size: var(--fs-headline);
}

.resume-document :deep(.resume-details) {
  display: flex;
  gap: var(--gap-inline);
}

.resume-document :deep(.details-inline) {
  flex-flow: row wrap;
  justify-content: inherit;
}

.resume-document :deep(.details-stacked) {
  flex-direction: column;
  gap: var(--gap-block);
}

.resume-document :deep(.contact-chip) {
  display: inline-flex;
  gap: 0.25em;
  align-items: center;
}

.resume-document :deep(.resume-icon) {
  width: var(--icon-size);
  height: var(--icon-size);
  flex: none;
}

.layout-two-columns {
  display: grid;
  grid-template-columns: minmax(0, 1fr) var(--sidebar-ratio);
  gap: var(--column-gutter);
}

.resume-sidebar {
  min-width: 0;
  background: var(--color-surface);
}

.resume-document :deep(.resume-section) {
  margin-block-end: var(--gap-section);
  break-inside: auto;
}

.resume-document :deep(.section-heading) {
  display: flex;
  gap: 0.35em;
  align-items: center;
  margin-block-end: var(--gap-heading);
  padding-block-end: var(--rule-gap);
  color: var(--color-heading);
  border-block-end: var(--rule-width) solid var(--color-rule);
  break-after: avoid;
}

.resume-document :deep(.section-heading h2) {
  margin: 0;
  font-size: var(--fs-heading);
  line-height: var(--lh-heading);
  letter-spacing: var(--heading-letter-spacing);
  text-transform: var(--heading-transform);
}

.resume-document :deep(.entry) {
  margin-block-end: var(--gap-entry);
  break-inside: auto;
}

.resume-document :deep(.entry-header) {
  break-inside: avoid;
  break-after: avoid;
}

.resume-document :deep(.entry-title) {
  color: var(--color-heading);
  font-size: var(--fs-title);
  font-weight: 700;
}

.resume-document :deep(.entry-subtitle) {
  font-size: var(--fs-subtitle);
}

.resume-document :deep(.entry-meta) {
  color: var(--color-meta);
  font-size: var(--fs-meta);
}

.resume-document :deep(.entry-body p),
.resume-document :deep(.entry-body li) {
  orphans: 2;
  widows: 2;
}

.resume-document :deep(.entry-body li),
.resume-document :deep(.level-widget) {
  break-inside: avoid;
}

.resume-document :deep(.level-tag) {
  display: inline-block;
  padding: var(--tag-padding);
  color: var(--color-on-accent);
  background: var(--color-accent-solid);
  border-radius: var(--tag-radius);
}

.resume-document :deep(.level-track) {
  display: block;
  width: 100%;
  height: var(--bar-height);
  overflow: hidden;
  background: var(--color-track);
  border-radius: var(--bar-radius);
}

.resume-document :deep(.level-fill) {
  display: block;
  height: 100%;
  background: var(--color-accent-solid);
}

.resume-document :deep(.level-dots) {
  display: inline-flex;
  gap: var(--dot-gap);
}

.resume-document :deep(.level-dot) {
  width: var(--dot-size);
  height: var(--dot-size);
  background: var(--color-track);
  border-radius: 50%;
}

.resume-document :deep(.level-dot.filled) {
  background: var(--color-accent-solid);
}
</style>
