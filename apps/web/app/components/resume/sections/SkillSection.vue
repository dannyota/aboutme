<script setup lang="ts">
import type { Customization, Section } from '@aboutme/schema';

import EntryHeader from '../primitives/EntryHeader.vue';
import LevelWidget from '../primitives/LevelWidget.vue';
import RichText from '../primitives/RichText.vue';
import SectionHeading from '../primitives/SectionHeading.vue';

defineProps<{
  section: Extract<Section, { sectionType: 'skill' }>;
  displayStyle: Customization['sectionDisplay']['skill']['style'];
}>();
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
      <EntryHeader
        v-if="entry.level !== undefined && displayStyle !== 'text'"
        :title="entry.name"
      >
        <template #meta-widget>
          <LevelWidget
            :name="entry.name || ''"
            :level="entry.level"
            :style="displayStyle"
          />
        </template>
      </EntryHeader>
      <EntryHeader
        v-else
        :title="entry.name"
      />
      <RichText
        v-if="entry.infoHtml"
        :html="entry.infoHtml"
      />
    </article>
  </section>
</template>
