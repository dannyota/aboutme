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
// The chrome typeface (ADR 0050, docs/design/web.md): body text on every
// application page sets it first in --font-sans, so it downloads on first
// paint with no preload today. `?url` resolves Vite's hashed build path,
// the same file `fonts.css` emits, so this adds no second copy.
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

// Only the homepage, Privacy Policy, Terms, Verify, and gallery are for
// search engines.
const indexable = computed(() => isIndexablePath(route.path));

useHead(
  computed(() => ({
    title: 'aboutme',
    meta: indexable.value ? [] : [{ name: 'robots', content: 'noindex' }],
    // Application pages only: isAppSurface already excludes the render
    // harness (/_harness/**); the renderer mounts inside these pages but
    // does not own the document head; the print worker and public resume
    // pages render their own HTML through separate server-side paths that
    // never mount this component.
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
