<script setup lang="ts">
import type { Resume } from '@aboutme/schema';
import { computed } from 'vue';

import type { components } from '../../api/generated/openapi';
import LayoutColumns from '../resume/LayoutColumns.vue';
import ResumeHeader from '../resume/ResumeHeader.vue';
import { resolveRenderModel } from '../resume/resolveRenderModel';

type PublicResume = components['schemas']['PublicResume'];

const props = defineProps<{ publicResume: PublicResume }>();

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

// The download control follows the resume's language, like the resume itself.
const downloadLabel = computed(() =>
  props.publicResume.lng.split('-')[0]?.toLowerCase() === 'vi'
    ? 'Tải PDF'
    : 'Download PDF');
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
    <div class="public-measure">
      <div
        v-if="publicResume.downloadEnabled"
        class="public-toolbar"
      >
        <a
          class="public-download"
          :href="downloadHref"
        >{{ downloadLabel }}</a>
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
          />
        </div>
        <LayoutColumns :model="model" />
      </article>
    </div>
  </div>
</template>
