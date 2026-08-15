<template>
  <div :class="{ 'aboutme-app': isAppSurface }">
    <header
      v-if="showAppChrome"
      class="app-chrome"
    >
      <NuxtLink
        class="app-brand"
        to="/"
      > aboutme </NuxtLink>
      <nav
        class="app-nav"
        aria-label="Primary navigation"
      >
        <NuxtLink to="/app/resumes"> Resumes </NuxtLink>
        <NuxtLink to="/app/settings/sessions"> Settings </NuxtLink>
      </nav>
      <div class="app-account-actions">
        <AccountControl v-if="isAuthenticatedSurface" />
        <ThemeToggle />
      </div>
    </header>
    <NuxtRouteAnnouncer v-if="isAppSurface" />
    <NuxtPage />
  </div>
</template>

<script setup lang="ts">
import AccountControl from './components/ui/AccountControl.vue';
import ThemeToggle from './components/ui/ThemeToggle.vue';

const route = useRoute();
const isAppSurface = computed(() => {
  const { path } = route;
  return (
    path === '/'
    || path === '/login'
    || path === '/app'
    || path.startsWith('/app/')
  );
});
const isAuthenticatedSurface = computed(
  () => route.path === '/app' || route.path.startsWith('/app/'),
);
const showAppChrome = computed(
  () => isAppSurface.value && !/^\/app\/resumes\/[^/]+$/.test(route.path),
);

useHead({
  title: 'aboutme',
});
</script>
