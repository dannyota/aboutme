<script setup lang="ts">
import LegalDocument from '@/components/legal/LegalDocument.vue';
import { legalCopy } from '@/i18n/legal';
import { pageTitle } from '@/i18n/meta';

const { locale } = useLocale();
const copy = computed(() => legalCopy[locale.value]);

useSiteSeo(() => ({
  path: '/privacy',
  title: pageTitle(copy.value.privacy.title),
  description: copy.value.privacy.description,
  locale: locale.value,
}));
</script>

<template>
  <LegalDocument
    :document="copy.privacy"
    testid="privacy-page"
    :updated="copy.updated"
  />
</template>
