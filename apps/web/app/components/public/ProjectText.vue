<script setup lang="ts">
import type {
  ProjectDetail,
} from '../../../server/utils/public-render/project-envelope';
import type { ProjectPageCopy } from '../../i18n/project-pages';
import ProjectDescription from './ProjectDescription';

// The project page's text column (docs/design/ui/project-pages.md, "Project
// page"): the outcome in a bordered panel, the description, and a link to
// the other projects. Each section is a region named by its heading.
defineProps<{
  readonly project: ProjectDetail;
  readonly copy: ProjectPageCopy;
  readonly listUrl: string;
  readonly otherProjects: boolean;
}>();
</script>

<template>
  <div class="project-text">
    <section
      v-if="project.outcome !== undefined"
      aria-labelledby="project-outcome-title"
      class="project-outcome"
    >
      <h2 id="project-outcome-title">
        {{ copy.outcome }}
      </h2>
      <p :lang="project.lang">
        {{ project.outcome }}
      </p>
    </section>
    <section
      v-if="project.description !== undefined"
      aria-labelledby="project-about-title"
      class="project-about"
    >
      <h2 id="project-about-title">
        {{ copy.about }}
      </h2>
      <ProjectDescription
        :html="project.description"
        :lang="project.lang"
      />
    </section>
    <p
      v-if="otherProjects"
      class="project-more"
    >
      <a :href="listUrl">{{ copy.otherProjects }}</a>
    </p>
  </div>
</template>
