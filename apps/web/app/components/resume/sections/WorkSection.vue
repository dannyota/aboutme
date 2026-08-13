<script setup lang="ts">
import type { Customization, Section } from '@aboutme/schema';

import { formatDateRange } from '../formatDate';
import EntryHeader from '../primitives/EntryHeader.vue';
import RichText from '../primitives/RichText.vue';
import SectionHeading from '../primitives/SectionHeading.vue';

withDefaults(defineProps<{
  section: Extract<Section, { sectionType: 'work' }>;
  dateFormat: Customization['dateFormat'];
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
        <EntryHeader
          :title="entry.jobTitle"
          :subtitle="entry.employer"
          :subtitle-link="entry.employerLink || undefined"
          :meta="[
            ...(entry.dates ? [formatDateRange(entry.dates, dateFormat)] : []),
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
