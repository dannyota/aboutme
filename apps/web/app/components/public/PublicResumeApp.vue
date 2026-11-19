<script setup lang="ts">
import type { Resume } from '@aboutme/schema';
import { computed, provide } from 'vue';

import type { components } from '../../api/generated/openapi';
import { ResumeLngKey } from '../resume/formatDate';
import LayoutColumns from '../resume/LayoutColumns.vue';
import {
  NO_PROJECT_LINKS,
  type ProjectLinkMap,
  ProjectLinksKey,
} from '../resume/projectLinks';
import { resolveRenderModel } from '../resume/resolveRenderModel';
import ResumeHeader from '../resume/ResumeHeader.vue';
import { articleStyle } from '../resume/useResumeStyles';
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

const colorScheme = computed<'dark' | 'system' | undefined>(() => {
  const scheme = props.publicResume.document.customization.colorScheme;
  return scheme === 'dark' || scheme === 'system' ? scheme : undefined;
});

const model = computed(() => resolveRenderModel(document.value, {
  lng: props.publicResume.lng,
  mode: 'continuous',
  ...(props.publicResume.document.personalDetails.photo === undefined
    ? {}
    : { photoUrl: props.publicResume.document.personalDetails.photo.url }),
}));
provide(ResumeLngKey, computed(() => model.value.lng));
const projectLinks = computed(() => props.projectLinks ?? NO_PROJECT_LINKS);
provide(ProjectLinksKey, projectLinks);
const rootStyle = computed(() => articleStyle(model.value.styles.root));

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
</script>

<template>
  <div
    class="public-resume-page"
    :data-color-scheme="colorScheme"
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
          :name-heading="model.nameHeading"
        />
      </div>
      <LayoutColumns :model="model" />
    </article>
  </div>
</template>
