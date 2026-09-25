<script setup lang="ts">
/**
 * The app-wide error page (Nuxt 4's app-level error handling: see
 * nuxt.com/docs/4.x/getting-started/error-handling). Nuxt renders this
 * instead of its own built-in error page, whose
 * error-404.vue/error-500.vue always emit an inline `<script>` (the Vite
 * modulepreload polyfill) that `script-src 'self'` blocks
 * (docs/design/security.md). This page renders no script of its own, and
 * never shows the error's own message or stack, in production or otherwise.
 */
import type { NuxtError } from '#app';

import { buttonVariants } from '@/components/ui/button';
import { errorCopy } from '@/i18n/error';
import { pageTitle } from '@/i18n/meta';

const props = defineProps<{ error: NuxtError }>();

const { locale } = useLocale();
const is404 = computed(() => props.error.statusCode === 404);
const content = computed(() => {
  const copy = errorCopy[locale.value];
  return is404.value ? copy.notFound : copy.generic;
});
const homeLabel = computed(() => errorCopy[locale.value].homeLink);

useHead(computed(() => ({
  title: pageTitle(content.value.title),
  htmlAttrs: { lang: locale.value },
  meta: [{ name: 'robots', content: 'noindex' }],
})));

function goHome(): void {
  clearError({ redirect: '/' });
}
</script>

<template>
  <main
    class="mx-auto flex min-h-screen w-full max-w-[42rem] flex-col
      items-center justify-center px-6 py-16 text-center"
    data-testid="error-page"
  >
    <h1
      class="text-2xl font-semibold"
      data-page-title
    >
      {{ content.title }}
    </h1>
    <p class="mt-4 text-base leading-7 text-muted-foreground">
      {{ content.description }}
    </p>
    <button
      :class="buttonVariants({ variant: 'default' })"
      class="mt-8"
      type="button"
      @click="goHome"
    >
      {{ homeLabel }}
    </button>
  </main>
</template>
