<script setup lang="ts">
import { computed } from 'vue';

import type {
  ProjectPageResume,
  ProjectSummary,
} from '../../../server/utils/public-render/project-envelope';
import { PROJECT_PAGE_COPY } from '../../i18n/project-pages';
import { formatDateRange } from '../resume/formatDate';
import ProjectChips from './ProjectChips.vue';
import ProjectPageBar from './ProjectPageBar.vue';

// The project list, /{slug}/projects (docs/design/ui/project-pages.md,
// "Project list"). Server-rendered only, with no script. Each row's project
// text carries the project's lang when it is not in the resume's language;
// dates follow the resume's format and language.
const props = defineProps<{
  readonly resume: ProjectPageResume;
  readonly projects: readonly ProjectSummary[];
  /** `{origin}/`, the credit's href. */
  readonly homeHref: string;
}>();

const copy = computed(() => PROJECT_PAGE_COPY[props.resume.uiLng]);

/** Role and dates on one muted line: the part after the role. */
const metaTail = (row: ProjectSummary): string => {
  if (row.dates === undefined) return '';
  const dates = formatDateRange(
    row.dates, props.resume.dateFormat, props.resume.lng);
  return row.role === undefined ? dates : ` · ${dates}`;
};
</script>

<template>
  <div class="public-project-page public-project-page-list">
    <a href="#public-project">Skip to content</a>
    <ProjectPageBar
      :home-href="homeHref"
      :owner-name="resume.ownerName"
      :resume-url="resume.url"
      :ui-lng="resume.uiLng"
    />
    <main
      id="public-project"
      class="public-measure project-list-page"
    >
      <h1>{{ copy.projects }}</h1>
      <p class="project-owner">
        <a :href="resume.url">{{ resume.ownerName }}</a>
      </p>
      <ul class="project-rows">
        <li
          v-for="row in projects"
          :key="row.slug"
          class="project-row"
        >
          <h2
            class="project-row-name"
            :lang="row.lang"
          >
            <a :href="row.url">{{ row.name }}</a>
          </h2>
          <p
            v-if="row.role !== undefined || row.dates !== undefined"
            class="project-row-meta"
          >
            <span
              v-if="row.role !== undefined"
              :lang="row.lang"
            >{{ row.role }}</span>{{ metaTail(row) }}
          </p>
          <p
            v-if="row.summary !== undefined"
            class="project-row-summary"
            :lang="row.lang"
          >
            {{ row.summary }}
          </p>
          <ProjectChips
            v-if="row.technologies.length > 0"
            :label="copy.technologies"
            :more="row.moreTechnologies"
            :technologies="row.technologies"
          />
        </li>
      </ul>
    </main>
  </div>
</template>
