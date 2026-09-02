<template>
  <div :class="{ 'aboutme-app': isAppSurface }">
    <AppChrome v-if="showAppChrome" />
    <NuxtRouteAnnouncer v-if="isAppSurface" />
    <NuxtPage />
  </div>
</template>

<script setup lang="ts">
import AppChrome from './components/ui/AppChrome.vue';

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
const showAppChrome = computed(
  () => isAppSurface.value && !/^\/app\/resumes\/[^/]+$/.test(route.path),
);

useHead({
  title: 'aboutme',
});
</script>
