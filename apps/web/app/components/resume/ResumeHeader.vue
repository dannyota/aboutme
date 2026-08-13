<script setup lang="ts">
import type { ResolvedRenderModel } from './resolveRenderModel';

import ContactChip from './primitives/ContactChip.vue';
import Photo from './primitives/Photo.vue';

defineProps<{
  personalDetails: ResolvedRenderModel['personalDetails'];
  header: ResolvedRenderModel['header'];
  photo?: ResolvedRenderModel['photo'];
}>();
</script>

<template>
  <header
    class="resume-header"
    :style="{
      textAlign: header.align,
      background: 'var(--color-surface)',
    }"
  >
    <Photo
      v-if="photo"
      :photo="photo"
    />
    <h1
      v-if="personalDetails.fullName"
      class="resume-name"
    >
      {{ personalDetails.fullName }}
    </h1>
    <p
      v-if="personalDetails.headline"
      class="resume-headline"
    >
      {{ personalDetails.headline }}
    </p>
    <div
      v-if="personalDetails.details.length > 0"
      class="resume-details"
      :class="`details-${header.detailsLayout}`"
    >
      <ContactChip
        v-for="detail in personalDetails.details"
        :key="detail.id"
        :detail="detail"
        :icon-style="header.iconStyle"
      />
    </div>
  </header>
</template>
