<script setup lang="ts">
import type { LandingCopy } from '../copy';
import HomeGlyph from './HomeGlyph.vue';

defineProps<{ readonly copy: LandingCopy }>();
</script>

<template>
  <div class="duo">
    <section
      aria-labelledby="landing-ai-title"
      class="card home-panel"
      data-testid="landing-ai"
    >
      <h2
        id="landing-ai-title"
        class="home-h2-card"
      >
        {{ copy.ai.title }}
      </h2>
      <p>{{ copy.ai.text }}</p>
      <ul class="tags">
        <li
          v-for="tag in copy.ai.tags"
          :key="tag"
        >
          <HomeGlyph name="check" />{{ tag }}
        </li>
        <li class="off">
          {{ copy.ai.ownerTag }}
        </li>
      </ul>
      <NuxtLink
        class="home-textlink card-link"
        data-testid="landing-ai-link"
        to="/guide/mcp"
      >{{ copy.ai.link }}</NuxtLink>
    </section>
    <section
      aria-labelledby="landing-data-title"
      class="card card--sun home-panel"
      data-testid="landing-data"
    >
      <span
        aria-hidden="true"
        class="home-coin data-coin"
      ><HomeGlyph name="mark" /></span>
      <h2
        id="landing-data-title"
        class="home-h2-card"
      >
        {{ copy.data.title }}
      </h2>
      <p>{{ copy.data.text }}</p>
      <ul class="tags">
        <li>
          <HomeGlyph name="check" />{{ copy.data.tag }}
        </li>
        <li class="off">
          {{ copy.data.offTag }}
        </li>
      </ul>
      <NuxtLink
        class="home-textlink card-link"
        data-testid="landing-data-link"
        to="/privacy"
      >{{ copy.data.link }}</NuxtLink>
    </section>
  </div>
</template>

<style scoped>
.duo {
  display: grid;
  gap: 16px;
  padding: 16px 0;
}

.card {
  position: relative;
  display: flex;
  flex-direction: column;
  align-items: flex-start;
  justify-content: center;
  overflow: hidden;
  background: var(--surface-mist);
}

.card--sun {
  background: var(--surface-sun);
}

.card > :not(.data-coin) {
  position: relative;
}

.card p {
  max-width: 36em;
  margin-top: 14px;
  color: var(--muted-foreground);
}

.tags {
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
  margin-top: 22px;
}

.tags li {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  padding: 9px 16px 9px 10px;
  border-radius: 999px;
  background: var(--card);
  font-size: 15px;
  font-weight: 700;
}

.tags svg {
  flex: none;
  width: 22px;
  height: 22px;
}

.tags .off {
  padding: 7px 16px;
  border: 2px dashed var(--control);
  background: transparent;
}

.card-link {
  display: inline-block;
  margin-top: 18px;
}

.data-coin {
  position: absolute;
  right: -42px;
  bottom: -52px;
  width: 150px;
  height: 150px;
  box-shadow: inset 0 -9px 0 var(--coin-edge);
  transform: rotate(14deg);
}

.data-coin svg {
  width: 78px;
}

@media (min-width: 48rem) {
  .duo {
    gap: 24px;
    padding: 24px 0;
  }
}

@media (min-width: 64rem) {
  .duo {
    grid-template-columns: minmax(0, 7fr) minmax(0, 5fr);
  }

  .data-coin {
    right: -40px;
    bottom: -46px;
    width: 124px;
    height: 124px;
  }

  .data-coin svg {
    width: 64px;
  }
}

@media (min-width: 80rem) {
  .data-coin {
    right: -56px;
    bottom: -58px;
    width: 190px;
    height: 190px;
  }

  .data-coin svg {
    width: 100px;
  }
}
</style>
