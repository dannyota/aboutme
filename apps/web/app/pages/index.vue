<script setup lang="ts">
import { computed } from 'vue';

import AppSeal from '@/components/app/AppSeal.vue';
import ResumeDocument from '@/components/resume/ResumeDocument.vue';
import { buttonVariants } from '@/components/ui/button';
import { legalCopy } from '@/i18n/legal';
import { homeTitle } from '@/i18n/meta';
import { landingCopy } from '@/landing/copy';
import { sampleContext } from '@/landing/sampleContext';
import { sampleLink, sampleResume } from '@/landing/sampleResume';
import { homeStructuredData } from '@/landing/structuredData';

// Every link below sits above the fold or just past it, so NuxtLink's
// default visibility-triggered prefetch would fetch /register, /templates,
// /login, /app/resumes, /terms, and /privacy on first paint whether or not
// the visitor follows any of them (docs/design/web.md). Interaction-only
// prefetch keeps the fast navigation on a real hover, focus, or touch
// without that unconditional network cost.
const visitPrefetch = { visibility: false, interaction: true };

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
  script: [{
    key: 'aboutme-structured-data',
    type: 'application/ld+json',
    innerHTML: homeStructuredData(copy.value.description),
  }],
})));
</script>

<template>
  <main
    class="landing mx-auto w-full max-w-7xl px-5 py-12 sm:px-8 sm:py-16"
    data-testid="landing"
  >
    <section
      class="grid items-start gap-12 min-[42rem]:grid-cols-12
        min-[42rem]:gap-8"
      aria-labelledby="landing-title"
    >
      <div class="min-[42rem]:col-span-5 min-[42rem]:pt-24">
        <h1
          id="landing-title"
          class="text-balance text-2xl font-bold leading-tight
            tracking-[-0.02em] min-[42rem]:text-3xl"
          data-testid="landing-title"
        >
          <span class="block">{{ copy.title[0] }}</span>{{ ' ' }}<span
            class="block"
          >{{ copy.title[1] }}</span>
        </h1>
        <p
          class="landing-lead mt-5 max-w-xl text-base leading-6
            text-muted-foreground"
        >
          {{ copy.lead }}
        </p>
        <div
          v-if="!signedIn"
          class="mt-7 flex flex-wrap items-center gap-3"
        >
          <NuxtLink
            :class="buttonVariants({ variant: 'default' })"
            :prefetch-on="visitPrefetch"
            data-testid="landing-create-account"
            to="/register"
          >{{ copy.createAccount }}</NuxtLink>
          <NuxtLink
            :class="buttonVariants({ variant: 'outline' })"
            :prefetch-on="visitPrefetch"
            data-testid="landing-browse-templates"
            to="/templates"
          >{{ copy.browseTemplates }}</NuxtLink>
          <NuxtLink
            class="text-sm text-primary underline-offset-4 hover:underline"
            :prefetch-on="visitPrefetch"
            data-testid="landing-sign-in"
            to="/login"
          >{{ copy.signIn }}</NuxtLink>
        </div>
        <div
          v-else
          class="mt-7 flex flex-wrap items-center gap-3"
        >
          <NuxtLink
            :class="buttonVariants({ variant: 'default' })"
            :prefetch-on="visitPrefetch"
            data-testid="landing-open-resumes"
            to="/app/resumes"
          >{{ copy.openResumes }}</NuxtLink>
          <NuxtLink
            :class="buttonVariants({ variant: 'outline' })"
            :prefetch-on="visitPrefetch"
            data-testid="landing-browse-templates"
            to="/templates"
          >{{ copy.browseTemplates }}</NuxtLink>
        </div>
      </div>

      <figure
        class="relative w-fit max-w-full min-w-0 justify-self-center
          min-[42rem]:col-span-7"
        :aria-label="copy.sampleLabel"
        data-testid="landing-sample"
      >
        <div
          class="landing-sheet rounded-[var(--radius-sheet)] bg-white
            shadow-[var(--shadow-paper)]"
          data-testid="landing-sheet"
        >
          <ResumeDocument
            :context="sampleContext"
            :document="sampleResume"
          />
        </div>
        <AppSeal
          :link="sampleLink"
          class="landing-seal"
          size="stamp"
        />
      </figure>
    </section>

    <ul
      class="landing-points mt-16 grid divide-y divide-border border-y
        border-border text-muted-foreground min-[42rem]:grid-cols-3
        min-[42rem]:divide-x min-[42rem]:divide-y-0"
    >
      <li
        v-for="point in copy.points"
        :key="point.title"
        class="py-4 min-[42rem]:px-5 min-[42rem]:first:pl-0
          min-[42rem]:last:pr-0"
        data-testid="landing-point"
      >
        <strong
          class="block font-medium text-foreground"
          data-testid="landing-point-title"
        >{{ point.title }}</strong>
        <span class="mt-1 block text-sm">{{ point.text }}</span>
      </li>
    </ul>

    <section
      class="mt-16"
      aria-labelledby="landing-publish-title"
    >
      <h2
        id="landing-publish-title"
        class="text-xl font-semibold tracking-tight"
      >
        {{ copy.publishTitle }}
      </h2>
      <dl
        class="mt-5 grid divide-y divide-border border-y border-border
          min-[42rem]:grid-cols-3 min-[42rem]:divide-x min-[42rem]:divide-y-0"
      >
        <div
          v-for="choice in copy.publishChoices"
          :key="choice.title"
          class="py-4 min-[42rem]:px-5 min-[42rem]:first:pl-0
            min-[42rem]:last:pr-0"
        >
          <dt class="font-medium">
            {{ choice.title }}
          </dt>
          <dd class="mt-1 text-sm text-muted-foreground">
            {{ choice.text }}
          </dd>
        </div>
      </dl>
    </section>

    <p
      class="mt-16 text-sm text-muted-foreground"
      data-testid="landing-license"
    >
      {{ copy.licensePrefix }}
      <a
        class="text-primary underline underline-offset-4"
        data-testid="landing-license-link"
        href="https://github.com/dannyota/aboutme"
        rel="noopener noreferrer"
      >AGPL-3.0</a>.
      <span aria-hidden="true"> · </span>
      <NuxtLink
        class="text-primary underline underline-offset-4"
        :prefetch-on="visitPrefetch"
        data-testid="landing-terms-link"
        to="/terms"
      >{{ legal.termsLink }}</NuxtLink>
      <span aria-hidden="true"> · </span>
      <NuxtLink
        class="text-primary underline underline-offset-4"
        :prefetch-on="visitPrefetch"
        data-testid="landing-privacy-link"
        to="/privacy"
      >{{ legal.privacyLink }}</NuxtLink>
    </p>
  </main>
</template>

<style scoped>
.landing-sheet {
  width: 210mm;
  min-height: 297mm;
  overflow: hidden;
  zoom: 0.6;
}

.landing-seal {
  position: absolute;
  right: -32px;
  bottom: 18px;
}

@media (max-width: 41.999rem) {
  .landing-sheet {
    zoom: 0.5;
  }

  /* The sheet fills the width here, so the seal lands on the blank foot
     of the main column instead of over the sidebar text. */
  .landing-seal {
    right: auto;
    left: 30%;
    bottom: 16px;
  }
}

@media (max-width: 390px) {
  .landing-sheet {
    zoom: 0.44;
  }
}

@media (prefers-reduced-motion: reduce) {
  .landing {
    scroll-behavior: auto;
  }
}
</style>
