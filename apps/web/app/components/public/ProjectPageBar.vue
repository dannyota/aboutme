<script setup lang="ts">
import { computed } from 'vue';

import {
  PROJECT_PAGE_COPY,
  type ProjectPageLanguage,
} from '../../i18n/project-pages';
import PublicMark from './PublicMark.vue';

const props = defineProps<{
  readonly uiLng: ProjectPageLanguage;
  readonly ownerName: string;
  readonly resumeUrl: string;
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
