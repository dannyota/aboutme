<script setup lang="ts">
import PublicDownloadLink from './PublicDownloadLink.vue';
import PublicMark from './PublicMark.vue';

defineProps<{
  homeHref: string;
  creditLabel: string;
  showProjects: boolean;
  projectsUrl?: string;
  projectsLabel: string;
  downloadEnabled: boolean;
  downloadHref: string;
  downloadLabel: string;
}>();
</script>

<template>
  <div class="public-toolbar">
    <div class="public-toolbar-inner">
      <span class="public-brand">
        <PublicMark />
        <a
          class="public-credit"
          :href="homeHref"
        >{{ creditLabel }}</a>
      </span>
      <span
        v-if="showProjects"
        class="public-actions"
      >
        <a
          class="public-projects"
          :href="projectsUrl"
        >{{ projectsLabel }}</a>
        <PublicDownloadLink
          v-if="downloadEnabled"
          :href="downloadHref"
          :label="downloadLabel"
        />
      </span>
      <PublicDownloadLink
        v-else-if="downloadEnabled"
        :href="downloadHref"
        :label="downloadLabel"
      />
    </div>
  </div>
</template>
