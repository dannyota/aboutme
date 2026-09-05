<script setup lang="ts">
import type { Resume } from '@aboutme/schema';
import { computed } from 'vue';

import type { components } from '../../api/generated/openapi';
import LayoutColumns from '../resume/LayoutColumns.vue';
import ResumeHeader from '../resume/ResumeHeader.vue';
import { resolveRenderModel } from '../resume/resolveRenderModel';
import '../resume/ResumeDocument.vue?vue&type=style&index=0&lang.css';

type PublicResumeDocument = components['schemas']['PublicResumeDocument'];

const props = defineProps<{
  document: PublicResumeDocument;
  lng: string;
}>();

const document = computed(() => {
  const source = props.document;
  return {
    ...source,
    personalDetails: {
      ...source.personalDetails,
      ...(source.personalDetails.photo === undefined
        ? {}
        : {
            photo: {
              ...source.personalDetails.photo,
              key: 'print-inline-photo',
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
  lng: props.lng,
  mode: 'continuous',
  ...(props.document.personalDetails.photo === undefined
    ? {}
    : { photoUrl: props.document.personalDetails.photo.url }),
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
