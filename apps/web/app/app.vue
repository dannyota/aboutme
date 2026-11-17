<template>
  <div :class="{ 'aboutme-app': isAppSurface }">
    <AppShell v-if="showAppChrome" />
    <NuxtRouteAnnouncer v-if="isAppSurface" />
    <NuxtPage />
    <SiteFooter v-if="showFooter" />
  </div>
</template>

<script setup lang="ts">
import AppShell from './components/app/AppShell.vue';
import SiteFooter from './components/app/SiteFooter.vue';
import { hasSiteFooter } from './components/app/footerRoutes';
import { themeColorMeta, useStoredTheme } from './composables/useTheme';
import { isIndexablePath } from './i18n/meta';
import plusJakartaSansUrl
  from './assets/fonts/plus-jakarta-sans-var.woff2?url';

const route = useRoute();
const isAppSurface = computed(() => !route.path.startsWith('/_harness'));
const showAppChrome = computed(
  () => isAppSurface.value && !/^\/app\/resumes\/[^/]+$/.test(route.path),
);
const showFooter = computed(
  () => isAppSurface.value && hasSiteFooter(route.path),
);
const theme = useStoredTheme();

const locale = useRouteLocale();

const indexable = computed(() => isIndexablePath(route.path));

useHead(
  computed(() => ({
    title: 'aboutme.vn',
    meta: [
      ...(indexable.value ? [] : [{ name: 'robots', content: 'noindex' }]),
      ...(isAppSurface.value ? themeColorMeta(theme.value) : []),
    ],
    link: isAppSurface.value
      ? [{
          rel: 'preload',
          as: 'font',
          type: 'font/woff2',
          href: plusJakartaSansUrl,
          crossorigin: 'anonymous',
        }]
      : [],
    htmlAttrs: isAppSurface.value
      ? {
          'lang': locale.value,
          'data-ui': 'app',
          ...(theme.value ? { 'data-theme': theme.value } : {}),
        }
      : {},
  })),
);
</script>
