<script setup lang="ts">
import type { Locale } from '@/i18n/locale';

import type { LandingCopy } from '../copy';
import HeroArt from './HeroArt.vue';

defineProps<{
  readonly copy: LandingCopy;
  readonly locale: Locale;
  readonly signedIn: boolean;
}>();
</script>

<template>
  <section
    aria-labelledby="landing-title"
    class="hero home-panel home-inverse"
    data-testid="landing-hero"
  >
    <div
      aria-hidden="true"
      class="rings"
      data-testid="landing-rings"
    >
      <svg viewBox="0 0 400 400">
        <g
          fill="none"
          stroke="var(--brand-hover)"
          transform="rotate(-8 200 200)"
        >
          <circle
            cx="200"
            cy="200"
            r="170"
            stroke-width="34"
          />
          <circle
            cx="200"
            cy="200"
            r="122"
            stroke-width="5"
          />
        </g>
      </svg>
    </div>
    <div class="hero-copy">
      <h1
        id="landing-title"
        class="home-h1"
        data-testid="landing-title"
      >
        {{ copy.hero.title }}
      </h1>
      <p class="hero-lead">
        {{ copy.hero.lead }}
      </p>
      <div
        v-if="signedIn"
        class="home-pills"
        data-testid="landing-hero-actions"
      >
        <NuxtLink
          class="home-pill home-pill--white"
          data-testid="landing-open-resumes"
          to="/app/resumes"
        >{{ copy.openResumes }}</NuxtLink>
        <NuxtLink
          class="home-pill home-pill--ghost"
          data-testid="landing-browse-templates"
          to="/templates"
        >{{ copy.browseTemplates }}</NuxtLink>
      </div>
      <div
        v-else
        class="home-pills"
        data-testid="landing-hero-actions"
      >
        <NuxtLink
          class="home-pill home-pill--white"
          data-testid="landing-create-account"
          to="/register"
        >{{ copy.createResume }}</NuxtLink>
        <NuxtLink
          class="home-pill home-pill--ghost"
          data-testid="landing-browse-templates"
          to="/templates"
        >{{ copy.browseTemplates }}</NuxtLink>
        <NuxtLink
          class="home-textlink hero-signin"
          data-testid="landing-sign-in"
          to="/login"
        >{{ copy.signIn }}</NuxtLink>
      </div>
    </div>
    <HeroArt
      :copy="copy"
      :locale="locale"
    />
  </section>
</template>

<style scoped>
.hero {
  position: relative;
  display: flow-root;
  margin-bottom: 112px;
  padding-bottom: 0;
  background: var(--brand);
  color: var(--brand-foreground);
}

.rings {
  position: absolute;
  inset: 0;
  overflow: hidden;
  border-radius: inherit;
  pointer-events: none;
}

.rings svg {
  position: absolute;
  top: -130px;
  right: -230px;
  width: 580px;
  max-width: none;
}

.hero-copy {
  position: relative;
  padding-top: 8px;
}

.hero-lead {
  max-width: 31em;
  margin-top: 18px;
  font-size: 16px;
  line-height: 1.6;
}

.hero-signin {
  align-self: center;
  padding: 6px 4px;
  color: var(--brand-foreground);
}

@media (min-width: 48rem) {
  .hero {
    margin-bottom: 136px;
  }

  .rings svg {
    top: -160px;
    right: -200px;
    width: 760px;
  }

  .hero-copy {
    padding-top: 0;
  }

  .hero-lead {
    font-size: 18px;
  }
}

@media (min-width: 64rem) {
  .hero {
    display: grid;
    grid-template-columns: minmax(0, 1fr) auto;
    gap: 44px;
    margin-bottom: 152px;
    padding: 56px 56px 0;
  }

  .hero-copy {
    align-self: center;
    padding-bottom: 64px;
  }
}

@media (min-width: 80rem) {
  .hero {
    grid-template-columns: minmax(0, 6fr) minmax(0, 5fr);
    gap: 48px;
    padding: 72px 72px 0;
  }

  .rings svg {
    top: -170px;
    right: -190px;
    width: 860px;
  }

  .hero-lead {
    font-size: 20px;
  }
}
</style>
