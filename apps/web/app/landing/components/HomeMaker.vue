<script setup lang="ts">
import { repositoryUrl } from '@/i18n/legal';

import type { LandingCopy } from '../copy';
import { sampleLink } from '../sampleResume';

defineProps<{
  readonly copy: LandingCopy;
  readonly signedIn: boolean;
}>();

const AVATAR = '/landing/maker-avatar-320.webp';
const AVATAR_SRCSET = '/landing/maker-avatar-320.webp 320w, '
  + '/landing/maker-avatar-640.webp 640w';
const AVATAR_SIZES = '(min-width: 64rem) 232px, (min-width: 48rem) 176px, '
  + '168px';
</script>

<template>
  <section
    aria-labelledby="landing-maker-title"
    class="maker home-panel"
    data-testid="landing-maker"
  >
    <div class="avatar">
      <img
        :alt="copy.maker.avatarAlt"
        data-testid="landing-maker-avatar"
        decoding="async"
        height="320"
        loading="lazy"
        :sizes="AVATAR_SIZES"
        :src="AVATAR"
        :srcset="AVATAR_SRCSET"
        width="320"
      >
    </div>
    <div>
      <h2
        id="landing-maker-title"
        class="home-h2"
      >
        {{ copy.maker.title }}
      </h2>
      <p class="maker-text">
        {{ copy.maker.text }}
      </p>
      <div
        class="home-pills maker-pills"
        data-testid="landing-maker-actions"
      >
        <NuxtLink
          v-if="signedIn"
          class="home-pill"
          data-testid="landing-maker-open-resumes"
          to="/app/resumes"
        >{{ copy.openResumes }}</NuxtLink>
        <NuxtLink
          v-else
          class="home-pill"
          data-testid="landing-maker-create"
          to="/register"
        >{{ copy.createResume }}</NuxtLink>
        <a
          class="home-pill home-pill--line"
          data-testid="landing-maker-resume"
          :href="sampleLink"
        >{{ copy.maker.seeResume }}</a>
        <a
          class="home-pill home-pill--line"
          data-testid="landing-source-link"
          :href="repositoryUrl"
          rel="noopener noreferrer"
        >{{ copy.maker.viewCode }}</a>
      </div>
    </div>
  </section>
</template>

<style scoped>
.maker {
  display: grid;
  justify-items: center;
  gap: 24px;
  margin-bottom: var(--space-section);
  background: var(--surface-blush);
  text-align: center;
}

.avatar {
  flex: none;
  width: 168px;
  height: 168px;
  overflow: hidden;
  border: 6px solid var(--chip);
  border-radius: 50%;
  background: var(--chip);
  box-shadow: var(--shadow-float);
}

.avatar img {
  display: block;
  width: 100%;
  height: 100%;
  object-fit: cover;
  transform: scale(1.03);
}

.maker-text {
  max-width: 40em;
  margin-top: 14px;
}

.maker-pills {
  align-items: stretch;
}

@media (min-width: 28rem) {
  .maker-pills {
    justify-content: center;
  }
}

@media (min-width: 48rem) {
  .maker {
    grid-template-columns: auto minmax(0, 1fr);
    align-items: center;
    justify-items: start;
    gap: 36px;
    text-align: left;
  }

  .avatar {
    width: 176px;
    height: 176px;
  }

  .maker-pills {
    justify-content: flex-start;
  }
}

@media (min-width: 64rem) {
  .maker {
    gap: 56px;
    margin-bottom: var(--space-section-lg);
  }

  .avatar {
    width: 232px;
    height: 232px;
  }
}
</style>
