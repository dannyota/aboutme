<script setup lang="ts">
import type { Customization, Section } from '@aboutme/schema';

import EntryHeader from '../primitives/EntryHeader.vue';
import LevelWidget from '../primitives/LevelWidget.vue';
import RichText from '../primitives/RichText.vue';
import SectionHeading from '../primitives/SectionHeading.vue';

withDefaults(defineProps<{
  section: Extract<Section, { sectionType: 'skill' }>;
  displayStyle: Customization['sectionDisplay']['skill']['style'];
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
    </template>
  </section>
</template>
