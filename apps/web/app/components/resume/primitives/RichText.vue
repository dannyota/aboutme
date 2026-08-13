<script setup lang="ts">
import { computed } from 'vue';

import { sanitizeRichText } from '../../../utils/sanitizeRichText';

const props = defineProps<{ html: string }>();
const safeHtml = computed(() => sanitizeRichText(props.html));
</script>

<template>
  <!-- eslint-disable vue/no-v-html -->
  <div
    class="rich-text entry-body"
    v-html="safeHtml"
  />
  <!-- eslint-enable vue/no-v-html -->
</template>
