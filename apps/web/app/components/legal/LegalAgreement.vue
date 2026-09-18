<script setup lang="ts">
/** The "you agree to the Terms and the Privacy Policy" line; no checkbox. */
import { legalCopy } from '@/i18n/legal';
import type { Locale } from '@/i18n/locale';

const props = defineProps<{ locale: Locale; testid: string }>();
const legal = computed(() => legalCopy[props.locale]);
</script>

<template>
  <p
    class="text-xs leading-5 text-muted-foreground"
    :data-testid="testid"
  >
    {{ legal.agreement[0] }}<NuxtLink
      class="text-primary underline underline-offset-4"
      to="/terms"
    >{{ legal.termsLink }}</NuxtLink>{{ legal.agreement[1] }}<NuxtLink
      class="text-primary underline underline-offset-4"
      to="/privacy"
    >{{ legal.privacyLink }}</NuxtLink>{{ legal.agreement[2] }}
  </p>
</template>
