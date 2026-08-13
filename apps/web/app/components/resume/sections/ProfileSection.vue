<script setup lang="ts">
import type { Section } from '@aboutme/schema';

import RichText from '../primitives/RichText.vue';
import SectionHeading from '../primitives/SectionHeading.vue';

withDefaults(defineProps<{
  section: Extract<Section, { sectionType: 'profile' }>;
  renderPart?: 'all' | 'heading' | 'entry';
}>(), { renderPart: 'all' });
</script>

<template>
  <section
    v-if="section.entries.some(entry => !entry.isHidden)"
    class="resume-section"
  >
    <SectionHeading
      v-if="renderPart !== 'entry'"
      :display-name="section.displayName"
      :icon-key="section.iconKey"
    />
    <template v-if="renderPart !== 'heading'">
      <article
        v-for="entry in section.entries.filter(item => !item.isHidden)"
        :key="entry.id"
        class="entry"
      >
        <RichText
          v-if="entry.text"
          :html="entry.text"
        />
      </article>
    </template>
  </section>
</template>
