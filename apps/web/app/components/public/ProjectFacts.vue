<script setup lang="ts">
import { computed } from 'vue';

import type {
  ProjectDetail,
} from '../../../server/utils/public-render/project-envelope';
import type { ProjectPageCopy } from '../../i18n/project-pages';
import ProjectChips from './ProjectChips.vue';
import ProjectLinkIcon from './ProjectLinkIcon.vue';

const props = defineProps<{
  readonly project: ProjectDetail;
  readonly copy: ProjectPageCopy;
  readonly entryLink?: string;
  readonly dates?: string;
}>();

interface ExternalLink {
  readonly kind: 'repository' | 'demo' | 'link';
  readonly href: string;
  readonly label: string;
  readonly primary: boolean;
}

const links = computed<ExternalLink[]>(() => {
  const { repositoryUrl, demoUrl } = props.project;
  return [
    ...(repositoryUrl === undefined
      ? []
      : [{
          kind: 'repository' as const, href: repositoryUrl,
          label: props.copy.repository, primary: true,
        }]),
    ...(demoUrl === undefined
      ? []
      : [{
          kind: 'demo' as const, href: demoUrl, label: props.copy.demo,
          primary: repositoryUrl === undefined,
        }]),
    ...(props.entryLink === undefined
      ? []
      : [{
          kind: 'link' as const, href: props.entryLink,
          label: props.copy.link, primary: false,
        }]),
  ];
});
</script>

<template>
  <div class="project-facts">
    <div
      v-if="links.length > 0"
      class="project-links"
    >
      <a
        v-for="link in links"
        :key="link.kind"
        :class="['project-external',
                 { 'project-external-primary': link.primary }]"
        :href="link.href"
        rel="noopener noreferrer nofollow"
      ><ProjectLinkIcon :kind="link.kind" />{{ link.label }}</a>
    </div>
    <dl
      v-if="project.role !== undefined || dates !== undefined"
      class="project-facts-list"
    >
      <div v-if="project.role !== undefined">
        <dt>{{ copy.role }}</dt>
        <dd :lang="project.lang">
          {{ project.role }}
        </dd>
      </div>
      <div v-if="dates !== undefined">
        <dt>{{ copy.dates }}</dt>
        <dd>{{ dates }}</dd>
      </div>
    </dl>
    <div v-if="project.technologies.length > 0">
      <h2>{{ copy.technologies }}</h2>
      <ProjectChips :technologies="project.technologies" />
    </div>
  </div>
</template>
