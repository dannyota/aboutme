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

// Only the homepage, Privacy Policy, Terms, and gallery are for search engines.
const indexable = computed(() => isIndexablePath(route.path));

useHead(
  computed(() => ({
    title: 'aboutme',
    meta: indexable.value ? [] : [{ name: 'robots', content: 'noindex' }],
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
