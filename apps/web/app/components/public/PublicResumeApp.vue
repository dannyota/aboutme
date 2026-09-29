<script setup lang="ts">
import type { Resume } from '@aboutme/schema';
import { computed } from 'vue';

import type { components } from '../../api/generated/openapi';
import LayoutColumns from '../resume/LayoutColumns.vue';
import ResumeHeader from '../resume/ResumeHeader.vue';
import { resolveRenderModel } from '../resume/resolveRenderModel';
import PublicMark from './PublicMark.vue';

type PublicResume = components['schemas']['PublicResume'];

const props = defineProps<{
  publicResume: PublicResume;
  /** The canonical home page, `<origin>/`, which the page credit links. */
  homeHref: string;
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

const model = computed(() => resolveRenderModel(document.value, {
  lng: props.publicResume.lng,
  mode: 'continuous',
  ...(props.publicResume.document.personalDetails.photo === undefined
    ? {}
    : { photoUrl: props.publicResume.document.personalDetails.photo.url }),
}));

// The page chrome follows the resume's language, like the resume itself.
const vietnamese = computed(() =>
  props.publicResume.lng.split('-')[0]?.toLowerCase() === 'vi');
const downloadLabel = computed(() =>
  vietnamese.value ? 'Tải PDF' : 'Download PDF');
// The server's public HTML validator requires exactly this credit anchor.
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
        <a
          v-if="publicResume.downloadEnabled"
          class="public-download"
          :href="downloadHref"
        ><svg
          aria-hidden="true"
          fill="none"
          focusable="false"
          height="16"
          stroke="currentColor"
          stroke-linecap="round"
          stroke-linejoin="round"
          stroke-width="2"
          viewBox="0 0 24 24"
          width="16"
          xmlns="http://www.w3.org/2000/svg"
        ><path d="M12 15V3" /><path
          d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"
        /><path d="m7 10 5 5 5-5" /></svg><span>{{ downloadLabel }}</span></a>
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
