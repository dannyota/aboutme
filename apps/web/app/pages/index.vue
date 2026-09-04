<script setup lang="ts">
import { computed } from 'vue';

import AppSeal from '@/components/app/AppSeal.vue';
import ResumeDocument from '@/components/resume/ResumeDocument.vue';
import { buttonVariants } from '@/components/ui/button';
import { sampleContext } from '@/landing/sampleContext';
import { sampleLink, sampleResume } from '@/landing/sampleResume';

const { authState } = useAuth();
const signedIn = computed(() => authState.value === 'authenticated');

useHead({
  title: 'aboutme',
  meta: [
    {
      name: 'description',
      content:
        'Open-source resume builder. Write once, preview the exact page '
        + 'layout, and publish each resume at a clean URL you control.',
    },
  ],
});

const points = [
  {
    title: 'Yours to keep.',
    text: 'Up to three resumes per account, private until you publish.',
  },
  {
    title: 'One link per resume.',
    text:
      'Publish, unpublish, and control search indexing for each resume '
      + 'on its own.',
  },
  {
    title: 'Bring your own agent.',
    text:
      'Connect an MCP-capable assistant with scopes you grant and can '
      + 'revoke.',
  },
] as const;
</script>

<template>
  <main class="landing mx-auto w-full max-w-7xl px-5 py-12 sm:px-8 sm:py-16">
    <section
      class="grid items-center gap-12 min-[42rem]:grid-cols-12
        min-[42rem]:gap-8"
      aria-labelledby="landing-title"
    >
      <div class="min-[42rem]:col-span-5">
        <h1
          id="landing-title"
          class="text-balance text-2xl font-bold leading-tight
            tracking-[-0.02em] min-[42rem]:text-3xl"
          data-testid="landing-title"
        >
          The resume is public. You are not.
        </h1>
        <p
          class="landing-lead mt-5 max-w-xl text-base leading-6
            text-muted-foreground"
        >
          aboutme is an open-source resume builder. Write up to three resumes,
          preview the exact page, and publish each one at its own link. Search
          and AI discovery stay off until you turn them on.
        </p>
        <div
          v-if="!signedIn"
          class="mt-7 flex flex-wrap items-center gap-3"
        >
          <NuxtLink
            :class="buttonVariants({ variant: 'default' })"
            data-testid="landing-create-account"
            to="/register"
          >Create account</NuxtLink>
          <NuxtLink
            class="text-sm text-primary underline-offset-4 hover:underline"
            data-testid="landing-sign-in"
            to="/login"
          >Sign in</NuxtLink>
        </div>
        <div
          v-else
          class="mt-7"
        >
          <NuxtLink
            :class="buttonVariants({ variant: 'default' })"
            data-testid="landing-open-resumes"
            to="/app/resumes"
          >Open your resumes</NuxtLink>
        </div>
      </div>

      <figure
        class="relative w-fit max-w-full min-w-0 justify-self-center
          min-[42rem]:col-span-7"
        aria-label="Sample resume published at aboutme.vn/ada-lovelace"
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
        v-for="point in points"
        :key="point.title"
        class="py-4 min-[42rem]:px-5 min-[42rem]:first:pl-0
          min-[42rem]:last:pr-0"
        data-testid="landing-point"
      >
        <strong
          class="block font-medium text-foreground"
          data-testid="landing-point-title"
        >{{ point.title }}</strong>
        <span>{{ point.text }}</span>
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
        Publishing is three choices
      </h2>
      <dl
        class="mt-5 grid divide-y divide-border border-y border-border
          min-[42rem]:grid-cols-3 min-[42rem]:divide-x min-[42rem]:divide-y-0"
      >
        <div
          class="py-4 min-[42rem]:px-5 min-[42rem]:first:pl-0
            min-[42rem]:last:pr-0"
        >
          <dt class="font-medium">
            Public resume
          </dt>
          <dd class="mt-1 text-sm text-muted-foreground">
            Whether any public page exists.
          </dd>
        </div>
        <div
          class="py-4 min-[42rem]:px-5 min-[42rem]:first:pl-0
            min-[42rem]:last:pr-0"
        >
          <dt class="font-medium">
            PDF download
          </dt>
          <dd class="mt-1 text-sm text-muted-foreground">
            Whether visitors can download the PDF. You can always export your
            own.
          </dd>
        </div>
        <div
          class="py-4 min-[42rem]:px-5 min-[42rem]:first:pl-0
            min-[42rem]:last:pr-0"
        >
          <dt class="font-medium">
            SEO and GEO
          </dt>
          <dd class="mt-1 text-sm text-muted-foreground">
            Whether search engines and AI answer engines may index it. Off by
            default.
          </dd>
        </div>
      </dl>
    </section>

    <p
      class="mt-16 text-center text-sm text-muted-foreground"
      data-testid="landing-license"
    >
      Open source under
      <a
        data-testid="landing-license-link"
        href="https://github.com/dannyota/aboutme"
        rel="noopener noreferrer"
      >AGPL-3.0</a>.
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

  .landing-seal {
    right: 0;
    bottom: 12px;
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
