<script setup lang="ts">
import { computed } from 'vue';

import type { Locale } from '@/i18n/locale';
import { galleryTemplate } from '@/templates/catalog';
import { samplePageImage } from '@/templates/pageImages';

import type { LandingCopy } from '../copy';
import { SHOWCASE } from '../showcase';

const props = defineProps<{
  readonly copy: LandingCopy;
  readonly locale: Locale;
}>();

const rows = computed(() => SHOWCASE.map((entry) => {
  const template = galleryTemplate(entry.id)!;
  return {
    id: entry.id,
    label: props.copy.templates.labels[entry.filter],
    name: template.name,
    purpose: `${template.purpose[props.locale]}.`,
    image: samplePageImage(entry.id, props.locale),
  };
}));
</script>

<template>
  <section
    aria-labelledby="landing-templates-title"
    class="tpls"
    data-testid="landing-templates"
  >
    <div class="tpl-head">
      <h2
        id="landing-templates-title"
        class="home-h2"
      >
        {{ copy.templates.title }}
      </h2>
      <p class="home-lead">
        {{ copy.templates.lead }}
      </p>
    </div>
    <div
      aria-hidden="true"
      class="fan"
      data-crop="fan"
      data-testid="landing-template-fan"
    >
      <template
        v-for="row in rows"
        :key="row.id"
      >
        <img
          v-if="row.image"
          alt=""
          :data-template="row.id"
          decoding="async"
          :height="row.image.height"
          loading="lazy"
          :src="row.image.src"
          :width="row.image.width"
        >
      </template>
      <span class="home-coin fan-coin" />
    </div>
    <div class="tpl-body">
      <ul
        class="tpl-list"
        data-testid="landing-template-list"
      >
        <li
          v-for="row in rows"
          :key="row.id"
        >
          <NuxtLink
            :data-template="row.id"
            :to="`/templates/${row.id}`"
          >
            <span class="tpl-label">{{ row.label }}</span>
            <span>
              <span class="tpl-name">{{ row.name }}</span>
              <span class="tpl-purpose">{{ row.purpose }}</span>
            </span>
          </NuxtLink>
        </li>
      </ul>
      <div class="home-pills">
        <NuxtLink
          class="home-pill home-pill--line"
          data-testid="landing-browse-all"
          to="/templates"
        >{{ copy.templates.openLibrary }}</NuxtLink>
      </div>
    </div>
  </section>
</template>

<style scoped>
.tpls {
  display: grid;
  gap: 32px;
  padding: var(--space-section) 8px;
}

.fan {
  position: relative;
  overflow: hidden;
  aspect-ratio: 1 / 0.8;
  border-radius: var(--radius-panel);
  background: var(--surface-blush);
}

.fan img {
  position: absolute;
  width: 40%;
  height: auto;
  border-radius: 2px;
  background: var(--paper);
  box-shadow: 0 22px 30px -20px rgba(0, 0, 0, 0.6);
}

.fan img:nth-child(1) {
  top: 21%;
  left: 3%;
  transform: rotate(-9deg);
}

.fan img:nth-child(2) {
  top: 12%;
  left: 20%;
  transform: rotate(-3deg);
}

.fan img:nth-child(3) {
  top: 12%;
  left: 38%;
  transform: rotate(3deg);
}

.fan img:nth-child(4) {
  top: 21%;
  left: 56%;
  transform: rotate(9deg);
}

.fan-coin {
  position: absolute;
  top: 6%;
  right: 5%;
  width: 46px;
  height: 46px;
}

.tpl-list {
  border-bottom: 1px solid var(--hairline);
}

.tpl-list li {
  border-top: 1px solid var(--hairline);
}

.tpl-list a {
  display: grid;
  gap: 2px 20px;
  margin: 0 -10px;
  padding: 15px 10px;
  text-decoration: none;
  transition: background-color 150ms;
}

.tpl-list a:hover {
  background: var(--surface-mist);
}

.tpl-label {
  color: var(--brand-text);
  font-size: 14px;
  font-weight: 700;
}

.tpl-name {
  display: block;
  font-size: 16px;
  font-weight: 800;
  line-height: 1.4;
}

.tpl-purpose {
  display: block;
  color: var(--muted-foreground);
  font-size: 15px;
  line-height: 1.5;
}

.tpl-body .home-pills {
  margin-top: 26px;
}

@media (min-width: 28rem) {
  .tpl-list a {
    grid-template-columns: 124px minmax(0, 1fr);
    align-items: baseline;
  }
}

@media (min-width: 48rem) {
  .tpls {
    padding: var(--space-section) 24px;
  }
}

@media (min-width: 64rem) {
  .tpls {
    grid-template-columns: minmax(0, 6fr) minmax(0, 5fr);
    gap: 0 64px;
    padding: var(--space-section-lg) 0;
  }

  .fan {
    grid-column: 1;
    grid-row: 1 / span 2;
    align-self: center;
    aspect-ratio: 1 / 0.84;
  }

  .tpl-head {
    grid-column: 2;
    align-self: end;
    padding-bottom: 26px;
  }

  .tpl-body {
    grid-column: 2;
    align-self: start;
  }
}

@media (min-width: 80rem) {
  .tpls {
    gap: 0 72px;
  }
}
</style>
