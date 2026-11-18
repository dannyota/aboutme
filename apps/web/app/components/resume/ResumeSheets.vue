<script setup lang="ts">
import { computed } from 'vue';

import type { ResolvedRenderModel } from './resolveRenderModel';
import ResumeHeader from './ResumeHeader.vue';
import SectionRenderer from './SectionRenderer.vue';
import { folioAttributes, groupSheets } from './sheetGroups';
import { articleStyle } from './useResumeStyles';

const props = defineProps<{
  model: ResolvedRenderModel;
}>();

const sheets = computed(() =>
  groupSheets([...props.model.main, ...props.model.sidebar]));
const rootStyle = computed(() => articleStyle(props.model.styles.root));
const band = computed(() => props.model.styles.header === undefined
  ? {}
  : { class: 'resume-sheet-band' });
</script>

<template>
  <article
    class="resume-document resume-sheets"
    :lang="model.lng"
    :style="rootStyle"
    :data-color-scheme="model.colorScheme"
  >
    <div
      v-for="(sheet, index) in sheets"
      :key="index"
      class="resume-sheet"
    >
      <div class="resume-sheet-body">
        <div
          v-if="sheet.header"
          v-bind="band"
          :style="model.styles.header"
        >
          <ResumeHeader
            :personal-details="model.personalDetails"
            :header="model.header"
            :photo="model.photo"
            :name-heading="model.nameHeading"
          />
        </div>
        <SectionRenderer
          v-for="item in sheet.sections"
          :key="item.key"
          :section="item.section"
          :date-format="model.dateFormat"
          :section-display="model.sectionDisplay"
        />
      </div>
      <div
        class="resume-folio"
        aria-hidden="true"
        v-bind="folioAttributes(
          model.personalDetails.fullName,
          index,
          sheets.length,
        )"
      />
    </div>
  </article>
</template>
