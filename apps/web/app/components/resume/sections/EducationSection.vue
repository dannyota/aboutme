<script setup lang="ts">
import type { Customization, Section } from '@aboutme/schema';

import { formatDateRange, useResumeLng } from '../formatDate';
import EntryHeader from '../primitives/EntryHeader.vue';
import RichText from '../primitives/RichText.vue';
import SectionHeading from '../primitives/SectionHeading.vue';

withDefaults(defineProps<{
  section: Extract<Section, { sectionType: 'education' }>;
  dateFormat: Customization['dateFormat'];
  renderPart?: 'all' | 'heading' | 'entry' | 'continuation';
}>(), { renderPart: 'all' });
const lng = useResumeLng();
</script>

<template>
  <section
    v-if="section.entries.some(entry => !entry.isHidden)"
    class="resume-section"
  >
    <SectionHeading
      v-if="renderPart === 'all' || renderPart === 'heading'"
      :display-name="section.displayName"
      :icon-key="section.iconKey"
    />
    <template v-if="renderPart !== 'heading'">
      <article
        v-for="entry in section.entries.filter(item => !item.isHidden)"
        :key="entry.id"
        class="entry"
      >
        <EntryHeader
          v-if="renderPart !== 'continuation'"
          :title="entry.degree"
          :subtitle="entry.school"
          :subtitle-link="entry.schoolLink || undefined"
          :meta="[
            ...(entry.dates
              ? [formatDateRange(entry.dates, dateFormat, lng)]
              : []),
            ...([entry.city, entry.country].filter(Boolean) as string[]),
          ]"
        />
        <RichText
          v-if="entry.description"
          :html="entry.description"
        />
      </article>
    </template>
  </section>
</template>
