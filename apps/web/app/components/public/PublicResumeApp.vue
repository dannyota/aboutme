<script setup lang="ts">
import type { Resume } from '@aboutme/schema';
import { computed, provide } from 'vue';

import type { components } from '../../api/generated/openapi';
import LayoutColumns from '../resume/LayoutColumns.vue';
import ResumeHeader from '../resume/ResumeHeader.vue';
import { ResumeLngKey } from '../resume/formatDate';
import {
  NO_PROJECT_LINKS,
  type ProjectLinkMap,
  ProjectLinksKey,
} from '../resume/projectLinks';
import { resolveRenderModel } from '../resume/resolveRenderModel';
import PublicDownloadLink from './PublicDownloadLink.vue';
import PublicMark from './PublicMark.vue';

type PublicResume = components['schemas']['PublicResume'];

const props = defineProps<{
  publicResume: PublicResume;
  homeHref: string;
  projectsUrl?: string;
  projectLinks?: ProjectLinkMap;
}>();

const document = computed(() => {
  const source = props.publicResume.document;
  return {
    ...source,
    personalDetails: {
      ...source.personalDetails,
      ...(source.personalDetails.photo === undefined
        ? {}
        : {
            photo: {
              ...source.personalDetails.photo,
              key: 'public-render-photo',
            },
          }),
      ...(source.personalDetails.details === undefined
        ? {}
        : {
            details: source.personalDetails.details.map((detail) => ({
              ...detail,
              isHidden: false,
            })),
          }),
    },
  } as unknown as Resume;
});

// Light, and an absent value, carry no attribute and no dark roles. The
// attribute and the render context take one of two fixed words, never the
// document's string (docs/design/public-page-theme.md, "Security, privacy,
// and size").
const colorScheme = computed<'dark' | 'system' | undefined>(() => {
  const scheme = props.publicResume.document.customization.colorScheme;
  return scheme === 'dark' || scheme === 'system' ? scheme : undefined;
});

const model = computed(() => resolveRenderModel(document.value, {
  lng: props.publicResume.lng,
  mode: 'continuous',
  ...(colorScheme.value === undefined
    ? {}
    : { colorScheme: colorScheme.value }),
  ...(props.publicResume.document.personalDetails.photo === undefined
    ? {}
    : { photoUrl: props.publicResume.document.personalDetails.photo.url }),
}));
provide(ResumeLngKey, computed(() => model.value.lng));
const projectLinks = computed(() => props.projectLinks ?? NO_PROJECT_LINKS);
provide(ProjectLinksKey, projectLinks);

const vietnamese = computed(() =>
  props.publicResume.lng.split('-')[0]?.toLowerCase() === 'vi');
const downloadLabel = computed(() =>
  vietnamese.value ? 'Tải PDF' : 'Download PDF');
const projectsLabel = computed(() =>
  vietnamese.value ? 'Dự án' : 'Projects');
const showProjects = computed(() =>
  props.projectsUrl !== undefined && projectLinks.value.size > 0);
const creditLabel = computed(() =>
  vietnamese.value ? 'Tạo bằng aboutme.vn' : 'Built with aboutme.vn');
const downloadHref = computed(() =>
  `/api/v1/public/resumes/${props.publicResume.slug}/pdf`);

const rootStyle = computed(() => ({
  ...model.value.styles.root,
  fontSynthesis: 'none',
  printColorAdjust: 'exact' as const,
  WebkitPrintColorAdjust: 'exact' as const,
}));
</script>

<template>
  <!-- The public page centres the resume at a readable measure; the editor
       preview and print never render this wrapper (docs/design/web.md). The
       article stays identical to the print document's. -->
  <div
    class="public-resume-page"
    :data-columns="model.columns"
    :data-color-scheme="colorScheme"
    :style="rootStyle"
  >
    <div class="public-toolbar">
      <div class="public-toolbar-inner">
        <span class="public-brand">
          <PublicMark />
          <a
            class="public-credit"
            :href="homeHref"
          >{{ creditLabel }}</a>
        </span>
        <span
          v-if="showProjects"
          class="public-actions"
        >
          <a
            class="public-projects"
            :href="projectsUrl"
          >{{ projectsLabel }}</a>
          <PublicDownloadLink
            v-if="publicResume.downloadEnabled"
            :href="downloadHref"
            :label="downloadLabel"
          />
        </span>
        <PublicDownloadLink
          v-else-if="publicResume.downloadEnabled"
          :href="downloadHref"
          :label="downloadLabel"
        />
      </div>
    </div>
    <div class="public-measure">
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
    </div>
  </div>
</template>
