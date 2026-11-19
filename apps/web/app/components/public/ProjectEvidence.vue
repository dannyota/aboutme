<script setup lang="ts">
import type { ProjectPageCopy } from '../../i18n/project-pages';
import type { EvidenceRow } from './evidenceRows';
import ProjectEvidenceIcon from './ProjectEvidenceIcon.vue';

defineProps<{
  readonly rows: readonly EvidenceRow[];
  readonly copy: ProjectPageCopy;
}>();
</script>

<template>
  <section
    aria-labelledby="project-evidence-title"
    class="project-evidence-section"
  >
    <h2 id="project-evidence-title">
      {{ copy.evidence }}
    </h2>
    <ul class="project-evidence-list">
      <li
        v-for="(row, index) in rows"
        :key="index"
        class="project-evidence-item"
      >
        <p
          v-if="row.label !== undefined"
          class="project-evidence-label"
        >
          <ProjectEvidenceIcon kind="confirmed" />{{ row.label }}
        </p>
        <p class="project-evidence-heading">
          <ProjectEvidenceIcon :kind="row.kind" /><a
            class="project-evidence"
            :href="row.url"
            rel="noopener noreferrer nofollow"
          >{{ row.title }}</a>
        </p>
        <p
          v-if="row.meta !== undefined"
          class="project-evidence-meta"
        >
          {{ row.meta }}
        </p>
      </li>
    </ul>
  </section>
</template>
