<script setup lang="ts">
/**
 * Static landing placeholder for the C1 web skeleton.
 *
 * Pure presentational component (props in, HTML out; no store/API access) —
 * mirrors the isolation rule the eventual `components/resume/` renderer must
 * follow (spec §5): safe to render identically on the server and client.
 */
withDefaults(
  defineProps<{
    title?: string;
    tagline?: string;
  }>(),
  {
    title: 'aboutme',
    tagline: 'Resume builder skeleton — SSR placeholder.',
  },
);
</script>

<template>
  <section
    class="placeholder-hero"
    aria-labelledby="placeholder-hero-title"
    data-testid="placeholder-hero"
  >
    <h1 id="placeholder-hero-title">
      {{ title }}
    </h1>
    <p>{{ tagline }}</p>
  </section>
</template>
