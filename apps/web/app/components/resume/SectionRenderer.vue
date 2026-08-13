<script setup lang="ts">
import type { Customization, Section } from '@aboutme/schema';
import { computed, type Component } from 'vue';

import CertificateSection from './sections/CertificateSection.vue';
import CustomSection from './sections/CustomSection.vue';
import EducationSection from './sections/EducationSection.vue';
import LanguageSection from './sections/LanguageSection.vue';
import ProfileSection from './sections/ProfileSection.vue';
import ProjectSection from './sections/ProjectSection.vue';
import SkillSection from './sections/SkillSection.vue';
import WorkSection from './sections/WorkSection.vue';

const props = defineProps<{
  section: Section;
  dateFormat: Customization['dateFormat'];
  sectionDisplay: Customization['sectionDisplay'];
}>();

const assertNever = (section: never): never => {
  const sectionType = (section as { sectionType?: unknown }).sectionType;
  throw new Error(`Unsupported section type: ${String(sectionType)}`);
};

type SectionView = {
  component: Component;
  props: Record<string, unknown>;
};

const view = computed<SectionView>(() => {
  const section = props.section;
  switch (section.sectionType) {
    case 'profile':
      return { component: ProfileSection, props: { section } };
    case 'work':
      return {
        component: WorkSection,
        props: { section, dateFormat: props.dateFormat },
      };
    case 'education':
      return {
        component: EducationSection,
        props: { section, dateFormat: props.dateFormat },
      };
    case 'skill':
      return {
        component: SkillSection,
        props: { section, displayStyle: props.sectionDisplay.skill.style },
      };
    case 'language':
      return {
        component: LanguageSection,
        props: { section, displayStyle: props.sectionDisplay.language.style },
      };
    case 'certificate':
      return {
        component: CertificateSection,
        props: { section, dateFormat: props.dateFormat },
      };
    case 'project':
      return {
        component: ProjectSection,
        props: { section, dateFormat: props.dateFormat },
      };
    case 'custom':
      return {
        component: CustomSection,
        props: { section, dateFormat: props.dateFormat },
      };
    default:
      return assertNever(section);
  }
});
</script>

<template>
  <component
    :is="view.component"
    v-bind="view.props"
  />
</template>
