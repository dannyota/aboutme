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

const rootStyle = computed(() => ({
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
