<script setup lang="ts">
import { computed } from 'vue';

import {
  PROJECT_PAGE_COPY,
  type ProjectPageLanguage,
} from '../../i18n/project-pages';
import PublicMark from './PublicMark.vue';

// The public page bar on a project page (docs/design/ui/project-pages.md,
// "Shared frame"): the credit on the left and one outlined link back to the
// resume, with no Download PDF. The credit anchor keeps exactly class, href
// and one text child, as Go's validator requires.
const props = defineProps<{
  readonly uiLng: ProjectPageLanguage;
  readonly ownerName: string;
  /** The resume page's absolute address. */
  readonly resumeUrl: string;
  /** `{origin}/`, the credit's href. */
  readonly homeHref: string;
}>();

const copy = computed(() => PROJECT_PAGE_COPY[props.uiLng]);
const backText = computed(() => `← ${props.ownerName}`);
</script>

<template>
  <div class="public-toolbar">
    <div class="public-toolbar-inner">
      <span class="public-brand">
        <PublicMark />
        <a
          class="public-credit"
          :href="homeHref"
        >{{ copy.credit }}</a>
      </span>
      <a
        :aria-label="copy.back(ownerName)"
        class="public-back"
        :href="resumeUrl"
      >{{ backText }}</a>
    </div>
  </div>
</template>
