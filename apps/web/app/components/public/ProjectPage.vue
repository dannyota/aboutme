<script setup lang="ts">
import { computed } from 'vue';

import type {
  ProjectDetail,
  ProjectPageResume,
} from '../../../server/utils/public-render/project-envelope';
import { PROJECT_PAGE_COPY } from '../../i18n/project-pages';
import { formatDateRange } from '../resume/formatDate';
import ProjectFacts from './ProjectFacts.vue';
import ProjectPageBar from './ProjectPageBar.vue';
import ProjectText from './ProjectText.vue';

const props = defineProps<{
  readonly resume: ProjectPageResume;
  readonly project: ProjectDetail;
  readonly entryLink?: string;
  readonly listUrl: string;
  readonly otherProjects: boolean;
  readonly homeHref: string;
}>();

const copy = computed(() => PROJECT_PAGE_COPY[props.resume.uiLng]);

const dates = computed(() => props.project.dates === undefined
  ? undefined
  : formatDateRange(
      props.project.dates, props.resume.dateFormat, props.resume.lng));

const hasFacts = computed(() => props.project.repositoryUrl !== undefined
  || props.project.demoUrl !== undefined
  || props.entryLink !== undefined
  || props.project.role !== undefined
  || dates.value !== undefined
  || props.project.technologies.length > 0);

const hasText = computed(() => props.project.outcome !== undefined
  || props.project.description !== undefined
  || props.otherProjects);
</script>

<template>
  <div class="public-project-page public-project-page-detail">
    <a href="#public-project">Skip to content</a>
    <ProjectPageBar
      :home-href="homeHref"
      :owner-name="resume.ownerName"
      :resume-url="resume.url"
      :ui-lng="resume.uiLng"
    />
    <main
      id="public-project"
      class="public-measure project-page"
    >
      <nav class="project-breadcrumb">
        <ol>
          <li><a :href="resume.url">{{ resume.ownerName }}</a></li>
          <li><a :href="listUrl">{{ copy.projects }}</a></li>
        </ol>
      </nav>
      <h1 :lang="project.lang">
        {{ project.name }}
      </h1>
      <p
        v-if="project.summary !== undefined"
        class="project-lead"
        :lang="project.lang"
      >
        {{ project.summary }}
      </p>
      <div
        v-if="hasFacts && hasText"
        class="project-columns"
      >
        <ProjectFacts
          :copy="copy"
          :dates="dates"
          :entry-link="entryLink"
          :project="project"
        />
        <ProjectText
          :copy="copy"
          :list-url="listUrl"
          :other-projects="otherProjects"
          :project="project"
        />
      </div>
      <ProjectFacts
        v-else-if="hasFacts"
        :copy="copy"
        :dates="dates"
        :entry-link="entryLink"
        :project="project"
      />
      <ProjectText
        v-else-if="hasText"
        :copy="copy"
        :list-url="listUrl"
        :other-projects="otherProjects"
        :project="project"
      />
    </main>
  </div>
</template>
