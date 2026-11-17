<script setup lang="ts">
import { computed } from 'vue';

import plusJakartaSansUrl from '@/assets/fonts/plus-jakarta-sans-var.woff2?url';
import { legalCopy } from '@/i18n/legal';
import { homeTitle } from '@/i18n/meta';
import HomeCards from '@/landing/components/HomeCards.vue';
import HomeEditor from '@/landing/components/HomeEditor.vue';
import HomeFooter from '@/landing/components/HomeFooter.vue';
import HomeHero from '@/landing/components/HomeHero.vue';
import HomeMaker from '@/landing/components/HomeMaker.vue';
import HomePublish from '@/landing/components/HomePublish.vue';
import HomeTemplates from '@/landing/components/HomeTemplates.vue';
import { landingCopy } from '@/landing/copy';
import { homeStructuredData } from '@/landing/structuredData';

const { authState } = useAuth();
const signedIn = computed(() => authState.value === 'authenticated');
const { locale } = useLocale();
const copy = computed(() => landingCopy[locale.value]);
const legal = computed(() => legalCopy[locale.value]);

useSiteSeo(() => ({
  path: '/',
  title: homeTitle[locale.value],
  description: copy.value.description,
  locale: locale.value,
}));
useHead(computed(() => ({
  htmlAttrs: { 'data-page': 'home' },
  link: [{
    rel: 'preload',
    as: 'font',
    type: 'font/woff2',
    href: plusJakartaSansUrl,
    crossorigin: 'anonymous',
  }],
  script: [{
    key: 'aboutme-structured-data',
    type: 'application/ld+json',
    innerHTML: homeStructuredData(copy.value.description),
  }],
})));
</script>

<template>
  <div
    class="home"
    data-testid="landing-page"
  >
    <main
      class="home-wrap"
      data-testid="landing"
    >
      <HomeHero
        :copy="copy"
        :locale="locale"
        :signed-in="signedIn"
      />
      <HomeEditor
        :copy="copy"
        :locale="locale"
      />
      <HomeTemplates
        :copy="copy"
        :locale="locale"
      />
      <HomePublish
        :copy="copy"
        :locale="locale"
      />
      <HomeCards :copy="copy" />
      <HomeMaker
        :copy="copy"
        :signed-in="signedIn"
      />
    </main>
    <HomeFooter
      :copy="copy"
      :legal="legal"
    />
  </div>
</template>

<style src="../landing/styles/home.css"></style>
