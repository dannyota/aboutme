<script setup lang="ts">
import { buttonVariants } from '@/components/ui/button';

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
    text: 'Publish, unpublish, and control search indexing for each resume '
      + 'on its own.',
  },
  {
    title: 'Bring your own agent.',
    text: 'Connect an MCP-capable assistant with scopes you grant and can '
      + 'revoke.',
  },
] as const;
</script>

<template>
  <main
    class="mx-auto flex min-h-[calc(100vh-3.5rem)] w-full max-w-3xl
      flex-col justify-center gap-12 px-5 py-16 sm:gap-16"
  >
    <section
      class="text-center"
      aria-labelledby="landing-title"
    >
      <h1
        id="landing-title"
        class="text-4xl font-semibold tracking-tight md:text-5xl"
      >
        Build your resume. Publish it at its own link.
      </h1>
      <p class="mx-auto mt-5 max-w-2xl text-lg text-muted-foreground">
        aboutme is an open-source resume builder. Write once, preview the exact
        page layout, and publish each resume at a clean URL you control.
      </p>
      <div class="mt-7 flex flex-wrap justify-center gap-2.5">
        <NuxtLink
          :class="buttonVariants({ variant: 'default' })"
          to="/login"
        >
          Sign in
        </NuxtLink>
        <NuxtLink
          :class="buttonVariants({ variant: 'outline' })"
          to="/register"
        >
          Create account
        </NuxtLink>
      </div>
    </section>
    <ul class="grid gap-4 text-muted-foreground">
      <li
        v-for="point in points"
        :key="point.title"
        data-testid="landing-point"
      >
        <strong
          class="font-medium text-foreground"
          data-testid="landing-point-title"
        >{{ point.title }}</strong>
        {{ point.text }}
      </li>
    </ul>
    <p
      class="text-center text-sm text-muted-foreground"
      data-testid="landing-license"
    >
      Open source under
      <a
        href="https://github.com/dannyota/aboutme"
        rel="noopener noreferrer"
      >AGPL-3.0</a>.
    </p>
  </main>
</template>
