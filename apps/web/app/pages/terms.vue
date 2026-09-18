<script setup lang="ts">
import LegalDocument from '@/components/legal/LegalDocument.vue';
import { legalCopy } from '@/i18n/legal';

const { locale } = useLocale();
const copy = computed(() => legalCopy[locale.value]);

useHead(computed(() => ({
  title: `${copy.value.terms.title} · aboutme`,
})));
</script>

<template>
  <LegalDocument
    :document="copy.terms"
    testid="terms-page"
    :updated="copy.updated"
  />
</template>
