<script setup lang="ts">
import { computed } from 'vue';

import { legalCopy } from '@/i18n/legal';
import HomeFooter from '@/landing/components/HomeFooter.vue';
import { landingCopy } from '@/landing/copy';

const { locale } = useLocale();
const copy = computed(() => landingCopy[locale.value]);
const legal = computed(() => legalCopy[locale.value]);
</script>

<template>
  <HomeFooter
    :copy="copy"
    :legal="legal"
  />
</template>
