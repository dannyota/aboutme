<script setup lang="ts">
import type { Resume } from '@aboutme/schema';
import { computed, type CSSProperties } from 'vue';

import LayoutColumns from './LayoutColumns.vue';
import PagedResume from './PagedResume.vue';
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
  <PagedResume
    v-if="context.mode === 'paged'"
    :document="document"
    :context="context"
  />
  <article
    v-else
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

<style>
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

.resume-document * {
  box-sizing: border-box;
}

.resume-document a {
  color: var(--color-link);
  text-decoration: underline;
}

.resume-document .resume-header {
  margin-block-end: var(--gap-section);
  break-inside: avoid;
}

.resume-document .resume-photo {
  width: var(--photo-size);
  height: var(--photo-size);
  border-radius: var(--photo-radius);
}

.resume-document .resume-name {
  margin: 0;
  color: var(--color-heading);
  font-size: var(--fs-name);
  line-height: var(--lh-heading);
}

.resume-document .resume-headline {
  margin: 0;
  color: var(--color-body);
  font-size: var(--fs-headline);
}

.resume-document .resume-details {
  display: flex;
  gap: var(--gap-inline);
}

.resume-document .details-inline {
  flex-flow: row wrap;
  justify-content: inherit;
}

.resume-document .details-stacked {
  flex-direction: column;
  gap: var(--gap-block);
}

.resume-document .contact-chip {
  display: inline-flex;
  gap: 0.25em;
  align-items: center;
  color: var(--color-body);
}

.resume-document .resume-icon {
  width: var(--icon-size);
  height: var(--icon-size);
  flex: none;
}

.resume-document .layout-two-columns {
  display: grid;
  grid-template-columns: minmax(0, 1fr) var(--sidebar-ratio);
  gap: var(--column-gutter);
}

.resume-document .resume-sidebar {
  min-width: 0;
  background: var(--color-surface);
}

.resume-document .resume-section {
  margin-block-end: var(--gap-section);
  break-inside: auto;
}

.resume-document .section-heading {
  display: flex;
  gap: 0.35em;
  align-items: center;
  margin-block-end: var(--gap-heading);
  padding-block-end: var(--rule-gap);
  color: var(--color-heading);
  border-block-end: var(--rule-width) solid var(--color-rule);
  break-after: avoid;
}

.resume-document .section-heading h2 {
  margin: 0;
  font-size: var(--fs-heading);
  line-height: var(--lh-heading);
  letter-spacing: var(--heading-letter-spacing);
  text-transform: var(--heading-transform);
}

.resume-document .entry {
  margin-block-end: var(--gap-entry);
  break-inside: auto;
}

.resume-document .entry-header {
  break-inside: avoid;
  break-after: avoid;
}

.resume-document .entry-title {
  color: var(--color-heading);
  font-size: var(--fs-title);
  font-weight: 700;
}

.resume-document .entry-subtitle {
  font-size: var(--fs-subtitle);
}

.resume-document .entry-meta {
  color: var(--color-meta);
  font-size: var(--fs-meta);
}

.resume-document .entry-body p,
.resume-document .entry-body li {
  orphans: 2;
  widows: 2;
}

.resume-document .entry-body li,
.resume-document .level-widget {
  break-inside: avoid;
}

.resume-document .level-tag {
  display: inline-block;
  padding: var(--tag-padding);
  color: var(--color-on-accent);
  background: var(--color-accent-solid);
  border-radius: var(--tag-radius);
}

.resume-document .level-track {
  display: block;
  width: 100%;
  height: var(--bar-height);
  overflow: hidden;
  background: var(--color-track);
  border-radius: var(--bar-radius);
}

.resume-document .level-fill {
  display: block;
  height: 100%;
  background: var(--color-accent-solid);
}

.resume-document .level-dots {
  display: inline-flex;
  gap: var(--dot-gap);
}

.resume-document .level-dot {
  width: var(--dot-size);
  height: var(--dot-size);
  background: var(--color-track);
  border-radius: 50%;
}

.resume-document .level-dot.filled {
  background: var(--color-accent-solid);
}

@media print {
  body.resume-print {
    margin: 0;
    padding: 0;
  }

  body.resume-print .resume-document {
    padding: 0;
  }
}
</style>
