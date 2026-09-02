<template>
  <div :class="{ 'aboutme-app': isAppSurface }">
    <AppChrome v-if="showAppChrome" />
    <NuxtRouteAnnouncer v-if="isAppSurface" />
    <NuxtPage />
  </div>
</template>

<script setup lang="ts">
import AppChrome from './components/app/AppChrome.vue';

const route = useRoute();
const isAppSurface = computed(() => !route.path.startsWith('/_harness'));
const showAppChrome = computed(
  () => isAppSurface.value && !/^\/app\/resumes\/[^/]+$/.test(route.path),
);

useHead(computed(() => ({
  title: 'aboutme',
  htmlAttrs: isAppSurface.value ? { 'data-ui': 'app' } : {},
})));
</script>
