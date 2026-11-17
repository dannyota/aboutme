<script setup lang="ts">
import type { NuxtError } from '#app';

import SiteFooter from '@/components/app/SiteFooter.vue';
import { buttonVariants } from '@/components/ui/button';
import { useStoredTheme } from '@/composables/useTheme';
import { errorCopy } from '@/i18n/error';
import { pageTitle } from '@/i18n/meta';

const props = defineProps<{ error: NuxtError }>();

const { locale } = useLocale();
const theme = useStoredTheme();
const is404 = computed(() => props.error.statusCode === 404);
const content = computed(() => {
  const copy = errorCopy[locale.value];
  return is404.value ? copy.notFound : copy.generic;
});
const homeLabel = computed(() => errorCopy[locale.value].homeLink);

useHead(computed(() => ({
  title: pageTitle(content.value.title),
  htmlAttrs: {
    'lang': locale.value,
    'data-ui': 'app',
    ...(theme.value ? { 'data-theme': theme.value } : {}),
  },
  meta: [{ name: 'robots', content: 'noindex' }],
})));

function goHome(): void {
  clearError({ redirect: '/' });
}
</script>

<template>
  <div class="flex min-h-screen flex-col">
    <main
      class="mx-auto flex w-full max-w-[44rem] flex-1 flex-col justify-center
        px-4 py-16 md:px-6"
      data-testid="error-page"
    >
      <div
        class="rounded-[var(--radius-panel)] bg-surface-mist px-6 py-12
          text-center md:px-12 md:py-14"
        data-testid="error-panel"
      >
        <h1
          class="text-2xl font-extrabold tracking-[-0.02em]"
          data-page-title
        >
          {{ content.title }}
        </h1>
        <p class="mt-4 text-md text-muted-foreground">
          {{ content.description }}
        </p>
        <button
          :class="buttonVariants({ variant: 'default', size: 'lg' })"
          class="mt-8"
          type="button"
          @click="goHome"
        >
          {{ homeLabel }}
        </button>
      </div>
    </main>
    <SiteFooter />
  </div>
</template>
