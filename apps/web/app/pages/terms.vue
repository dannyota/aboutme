<script setup lang="ts">
import LegalDocument from '@/components/legal/LegalDocument.vue';
import { legalCopy } from '@/i18n/legal';
import { pageTitle } from '@/i18n/meta';

const { locale } = useLocale();
const copy = computed(() => legalCopy[locale.value]);

useSiteSeo(() => ({
  path: '/terms',
  title: pageTitle(copy.value.terms.title),
  description: copy.value.terms.description,
  locale: locale.value,
}));
</script>

<template>
  <LegalDocument
    :document="copy.terms"
    testid="terms-page"
    :updated="copy.updated"
  />
</template>
