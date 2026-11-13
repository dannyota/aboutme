<script setup lang="ts">
import type { Resume } from '@aboutme/schema';
import { computed, type CSSProperties, provide } from 'vue';

import { ResumeLngKey } from './formatDate';
import LayoutColumns from './LayoutColumns.vue';
import PagedResume from './PagedResume.vue';
import { NO_PROJECT_LINKS, ProjectLinksKey } from './projectLinks';
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
provide(ResumeLngKey, computed(() => model.value.lng));
provide(ProjectLinksKey, computed(() =>
  props.context.projectLinks ?? NO_PROJECT_LINKS));
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
        :name-heading="model.nameHeading"
      />
    </div>
    <LayoutColumns :model="model" />
  </article>
</template>

<style>
@import './resumeColorScheme.css';

@import './resumeScreenMargins.css';

a[href="#public-resume"]:not(:focus) {
  position: absolute;
  width: 1px;
  height: 1px;
  margin: -1px;
  padding: 0;
  overflow: hidden;
  clip-path: inset(50%);
  white-space: nowrap;
  border: 0;
}

a[href="#public-resume"]:focus {
  position: absolute;
  top: 0.5rem;
  left: 0.5rem;
  z-index: 1;
  padding: 0.5rem 0.75rem;
  border-radius: 4px;
  background: #ffffff;
  color: #10202a;
  font: 600 14px/1.2 system-ui, sans-serif;
  outline: 2px solid #10202a;
}

body:has(> #public-resume) {
  margin: 0;
}

.public-resume-page {
  box-sizing: border-box;
  min-height: 100vh;
  background: var(--color-surface);
}

.public-measure {
  box-sizing: border-box;
  max-width: calc(52em + 2 * var(--page-margin-x));
  margin-inline: auto;
  font-size: var(--fs-body);
}

.public-resume-page[data-columns="2"] .public-measure {
  max-width: calc(70em + 2 * var(--page-margin-x));
}

.public-toolbar {
  --toolbar-ground: #f9f8f5;
  --toolbar-border: #e5e1d6;
  --toolbar-mark: #cc2649;
  --toolbar-credit: #5c6178;
  --toolbar-credit-hover: #23399a;
  --toolbar-button-fill: #ffffff;
  --toolbar-button-border: #e5e1d6;
  --toolbar-button-label: #23399a;
  --toolbar-button-hover-fill: #eceef6;
  --toolbar-focus-ring: #26409c;
  --toolbar-button-shadow: 0 1px 2px rgba(16, 27, 63, 0.06);
  box-sizing: border-box;
  padding-block: 8px 7px;
  background: var(--toolbar-ground);
  border-bottom: 1px solid var(--toolbar-border);
}

.public-toolbar-inner {
  box-sizing: border-box;
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: space-between;
  column-gap: 16px;
  row-gap: 8px;
  min-height: 32px;
  max-width: calc(52em + 2 * var(--page-margin-x));
  margin-inline: auto;
  padding-inline: var(--screen-margin-x);
  font-size: var(--fs-body);
}

.public-resume-page[data-columns="2"] .public-toolbar-inner {
  max-width: calc(70em + 2 * var(--page-margin-x));
}

@media (width < 40rem) {
  .public-toolbar-inner {
    column-gap: 12px;
  }
}

@media (pointer: coarse) {
  .public-toolbar-inner {
    min-height: 40px;
  }
}

.public-brand {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  min-width: 0;
}

.public-mark {
  flex-shrink: 0;
  color: var(--toolbar-mark);
}

.public-credit {
  font: 500 13px/20px "Be Vietnam Pro", system-ui, sans-serif;
  color: var(--toolbar-credit);
  text-decoration: none;
}

@media (hover: hover) {
  .public-credit:hover {
    color: var(--toolbar-credit-hover);
    text-decoration: underline;
    text-underline-offset: 3px;
  }
}

.public-credit:focus-visible {
  outline: 2px solid var(--toolbar-focus-ring);
  outline-offset: 2px;
}

@media (pointer: coarse) {
  .public-credit {
    display: inline-block;
    padding-block: 10px;
    margin-block: -10px;
  }
}

.public-actions {
  display: inline-flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 8px;
  min-width: 0;
}

.public-download,
.public-projects {
  box-sizing: border-box;
  display: inline-flex;
  flex-shrink: 0;
  align-items: center;
  gap: 6px;
  height: 32px;
  padding: 0 12px 0 10px;
  border: 1px solid var(--toolbar-button-border);
  border-radius: 10px;
  background: var(--toolbar-button-fill);
  color: var(--toolbar-button-label);
  font: 500 14px/20px "Be Vietnam Pro", system-ui, sans-serif;
  text-decoration: none;
  white-space: nowrap;
  box-shadow: var(--toolbar-button-shadow);
}

.public-projects {
  padding-inline: 12px;
}

.public-download svg {
  flex-shrink: 0;
}

@media (pointer: coarse) {
  .public-download,
  .public-projects {
    position: relative;
    height: 40px;
  }

  .public-download::before,
  .public-projects::before {
    content: "";
    position: absolute;
    inset: -3px -1px;
  }
}

@media (hover: hover) {
  .public-download:hover,
  .public-projects:hover {
    background: var(--toolbar-button-hover-fill);
  }
}

.public-download:focus-visible,
.public-projects:focus-visible {
  outline: 2px solid var(--toolbar-focus-ring);
  outline-offset: 2px;
}

@media (forced-colors: active) {
  .public-mark {
    color: CanvasText;
  }

  .public-download,
  .public-projects {
    border-color: ButtonBorder;
  }
}

@media print {
  .public-toolbar {
    display: none;
  }
}

.resume-document {
  box-sizing: border-box;
  min-height: 100%;
  font-variant-ligatures: no-common-ligatures;
  padding: var(--page-margin-y) var(--page-margin-x);
  color: var(--color-body);
  background: var(--color-surface);
  font-family: var(--font-family);
  font-size: var(--fs-body);
  line-height: var(--lh-body);
  overflow-wrap: anywhere;
}

.resume-document:not(.resume-page) {
  container-type: inline-size;
}

.resume-document * {
  box-sizing: border-box;
}

.resume-document a {
  color: var(--color-link);
  text-decoration: underline;
}

.resume-document .resume-header {
  display: grid;
  grid-template-columns: minmax(0, 1fr);
  padding: var(--header-padding);
  margin-block-end: var(--gap-header);
  break-inside: avoid;
}

.resume-document .resume-photo {
  position: relative;
  display: block;
  justify-self: var(--header-align);
  width: var(--photo-size);
  height: var(--photo-size);
  margin-block-end: var(--header-photo-gap);
  overflow: hidden;
  border-radius: var(--photo-radius);
}

.resume-document .resume-header-text {
  min-width: 0;
}

.resume-document .resume-header[data-photo-position] {
  grid-template-columns: var(--photo-size) minmax(0, 1fr);
  column-gap: var(--header-photo-gap-side);
  align-items: center;
}

.resume-document .resume-header[data-photo-position="right"] {
  grid-template-columns: minmax(0, 1fr) var(--photo-size);
}

.resume-document .resume-header[data-photo-position] .resume-photo {
  justify-self: auto;
  margin: 0;
}

.resume-document .resume-header[data-photo-position="right"] .resume-photo {
  grid-row: 1;
  grid-column: 2;
}

@container (width < 36em) {
  .resume-document:not(.resume-page) .resume-header[data-photo-position] {
    grid-template-columns: minmax(0, 1fr);
  }

  .resume-document:not(.resume-page)
    .resume-header[data-photo-position] .resume-photo {
    grid-row: auto;
    grid-column: auto;
    justify-self: var(--header-align);
    margin-block-end: var(--header-photo-gap);
  }
}

.resume-document .resume-photo-image {
  position: absolute;
  display: block;
  max-width: none;
  max-height: none;
  object-fit: cover;
}

.resume-document .resume-name {
  margin: 0;
  color: var(--color-heading);
  font-size: var(--fs-name);
  font-weight: 700;
  line-height: var(--lh-heading);
}

.resume-document .resume-headline {
  margin: var(--header-name-gap) 0 0;
  color: var(--color-body);
  font-size: var(--fs-headline);
}

.resume-document .resume-details {
  display: flex;
  gap: var(--details-row-gap) var(--details-column-gap);
  margin-block-start: var(--header-details-gap);
}

.resume-document .details-inline {
  flex-flow: row wrap;
  justify-content: var(--header-align);
}

.resume-document .details-stacked {
  display: grid;
  justify-items: var(--header-align);
}

.resume-document .contact-chip {
  display: inline-block;
  color: var(--color-body);
}

.resume-document .contact-chip:has(> .resume-icon) {
  padding-inline-start: calc(var(--icon-size) + var(--chip-icon-gap));
  text-indent: calc(-1 * (var(--icon-size) + var(--chip-icon-gap)));
}

.resume-document .contact-chip .resume-icon {
  margin-inline-end: var(--chip-icon-gap);
  vertical-align: -0.15em;
}

.resume-document .contact-label {
  white-space: nowrap;
}

.resume-document .contact-label::after {
  content: " ";
  white-space: normal;
}

.resume-document .resume-icon {
  width: var(--icon-size);
  height: var(--icon-size);
  flex: none;
  color: var(--color-meta);
}

.resume-document .resume-header a {
  text-decoration-thickness: 0.06em;
  text-underline-offset: 0.18em;
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

@container (width < 36em) {
  .resume-document:not(.resume-page) .layout-two-columns {
    grid-template-columns: minmax(0, 1fr);
  }
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
}

.resume-document .entry-header:not(:last-child) {
  break-after: avoid;
}

.resume-document .layout-two-columns {
  break-before: avoid;
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

.resume-document .entry-header + .entry-body {
  margin-block-start: var(--gap-block);
}

.resume-document .entry-body p {
  margin: 0 0 0.5em;
}

.resume-document .entry-body ul,
.resume-document .entry-body ol {
  margin: 0.35em 0;
  padding-inline-start: 1.25em;
}

.resume-document .entry-body li > p,
.resume-document .entry-body li > ul,
.resume-document .entry-body li > ol {
  margin: 0;
}

.resume-document .entry-body li + li {
  margin-block-start: 0.25em;
}

.resume-document .entry-body > :first-child {
  margin-block-start: 0;
}

.resume-document .entry-body > :last-child {
  margin-block-end: 0;
}

.resume-document .entry-body p,
.resume-document .entry-body li {
  orphans: 2;
  widows: 2;
  text-align: var(--body-align);
  hyphens: var(--body-hyphens);
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
