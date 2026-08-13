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

const props = withDefaults(defineProps<{
  section: Section;
  dateFormat: Customization['dateFormat'];
  sectionDisplay: Customization['sectionDisplay'];
  renderPart?: 'all' | 'heading' | 'entry';
}>(), { renderPart: 'all' });

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
  const renderPart = props.renderPart;
  switch (section.sectionType) {
    case 'profile':
      return { component: ProfileSection, props: { section, renderPart } };
    case 'work':
      return {
        component: WorkSection,
        props: { section, dateFormat: props.dateFormat, renderPart },
      };
    case 'education':
      return {
        component: EducationSection,
        props: { section, dateFormat: props.dateFormat, renderPart },
      };
    case 'skill':
      return {
        component: SkillSection,
        props: {
          section,
          displayStyle: props.sectionDisplay.skill.style,
          renderPart,
        },
      };
    case 'language':
      return {
        component: LanguageSection,
        props: {
          section,
          displayStyle: props.sectionDisplay.language.style,
          renderPart,
        },
      };
    case 'certificate':
      return {
        component: CertificateSection,
        props: { section, dateFormat: props.dateFormat, renderPart },
      };
    case 'project':
      return {
        component: ProjectSection,
        props: { section, dateFormat: props.dateFormat, renderPart },
      };
    case 'custom':
      return {
        component: CustomSection,
        props: { section, dateFormat: props.dateFormat, renderPart },
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
