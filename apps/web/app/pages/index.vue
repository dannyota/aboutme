<script setup lang="ts">
import '~/assets/css/landing.css';

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
  <main class="app-page landing">
    <section
      class="landing-hero"
      aria-labelledby="landing-title"
    >
      <h1 id="landing-title">
        Build your resume. Publish it at its own link.
      </h1>
      <p class="landing-lead">
        aboutme is an open-source resume builder. Write once, preview the exact
        page layout, and publish each resume at a clean URL you control.
      </p>
      <div class="landing-actions">
        <NuxtLink
          class="landing-button landing-button--primary"
          to="/login"
        >
          Sign in
        </NuxtLink>
        <NuxtLink
          class="landing-button"
          to="/register"
        >
          Create account
        </NuxtLink>
      </div>
    </section>
    <ul class="landing-points">
      <li
        v-for="point in points"
        :key="point.title"
        data-testid="landing-point"
      >
        <strong>{{ point.title }}</strong>
        {{ point.text }}
      </li>
    </ul>
    <p
      class="landing-license"
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
