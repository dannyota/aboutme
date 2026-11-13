<template>
  <div :class="{ 'aboutme-app': isAppSurface }">
    <AppShell v-if="showAppChrome" />
    <NuxtRouteAnnouncer v-if="isAppSurface" />
    <NuxtPage />
  </div>
</template>

<script setup lang="ts">
import AppShell from './components/app/AppShell.vue';
import type { Theme } from './composables/useTheme';
import { isIndexablePath } from './i18n/meta';
import beVietnamProUrl from './assets/fonts/be-vietnam-pro-var.woff2?url';

const route = useRoute();
const isAppSurface = computed(() => !route.path.startsWith('/_harness'));
const showAppChrome = computed(
  () => isAppSurface.value && !/^\/app\/resumes\/[^/]+$/.test(route.path),
);
const themePreference = useCookie<Theme | undefined>('aboutme-theme', {
  default: () => undefined,
  path: '/',
  sameSite: 'lax',
  watch: false,
});
const theme = computed(() => {
  const value = themePreference.value;
  return value === 'light' || value === 'dark' ? value : undefined;
});

const locale = useRouteLocale();

const indexable = computed(() => isIndexablePath(route.path));

useHead(
  computed(() => ({
    title: 'aboutme.vn',
    meta: indexable.value ? [] : [{ name: 'robots', content: 'noindex' }],
    link: isAppSurface.value
      ? [{
          rel: 'preload',
          as: 'font',
          type: 'font/woff2',
          href: beVietnamProUrl,
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
