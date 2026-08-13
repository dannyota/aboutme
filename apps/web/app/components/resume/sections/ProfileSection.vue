<script setup lang="ts">
import type { Section } from '@aboutme/schema';

import RichText from '../primitives/RichText.vue';
import SectionHeading from '../primitives/SectionHeading.vue';

defineProps<{ section: Extract<Section, { sectionType: 'profile' }> }>();
</script>

<template>
  <section
    v-if="section.entries.some(entry => !entry.isHidden)"
    class="resume-section"
  >
    <SectionHeading
      :display-name="section.displayName"
      :icon-key="section.iconKey"
    />
    <article
      v-for="entry in section.entries.filter(candidate => !candidate.isHidden)"
      :key="entry.id"
      class="entry"
    >
      <RichText
        v-if="entry.text"
        :html="entry.text"
      />
    </article>
  </section>
</template>
