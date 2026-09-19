<script setup lang="ts">
import type { ResolvedRenderModel } from './resolveRenderModel';

import ContactChip from './primitives/ContactChip.vue';
import Photo from './primitives/Photo.vue';

// A left or right photo position applies only when a photo renders; the
// name, headline, and details sit in one text block beside it (ADR 0044).
defineProps<{
  personalDetails: ResolvedRenderModel['personalDetails'];
  header: ResolvedRenderModel['header'];
  photo?: ResolvedRenderModel['photo'];
}>();
</script>

<template>
  <header
    class="resume-header"
    :data-photo-position="photo && header.photoPosition !== 'top'
      ? header.photoPosition
      : undefined"
    :style="{
      textAlign: header.align,
      background: 'var(--color-surface)',
    }"
  >
    <Photo
      v-if="photo"
      :photo="photo"
    />
    <div class="resume-header-text">
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
    </div>
  </header>
</template>
