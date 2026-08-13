<script setup lang="ts">
import type { ResolvedRenderModel } from './resolveRenderModel';

import SectionRenderer from './SectionRenderer.vue';

defineProps<{
  model: ResolvedRenderModel;
}>();
</script>

<template>
  <div
    v-if="model.columns === 1"
    class="layout-one-column"
  >
    <SectionRenderer
      v-for="item in [...model.main, ...model.sidebar]"
      :key="item.key"
      :section="item.section"
      :date-format="model.dateFormat"
      :section-display="model.sectionDisplay"
    />
  </div>
  <div
    v-else
    class="layout-two-columns"
  >
    <main class="resume-main">
      <SectionRenderer
        v-for="item in model.main"
        :key="item.key"
        :section="item.section"
        :date-format="model.dateFormat"
        :section-display="model.sectionDisplay"
      />
    </main>
    <aside
      class="resume-sidebar"
      :style="model.styles.sidebar"
    >
      <SectionRenderer
        v-for="item in model.sidebar"
        :key="item.key"
        :section="item.section"
        :date-format="model.dateFormat"
        :section-display="model.sectionDisplay"
      />
    </aside>
  </div>
</template>
