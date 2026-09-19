<script setup lang="ts">
import { computed } from 'vue';

import TemplateCard from '@/components/templates/TemplateCard.vue';
import { pageTitle } from '@/i18n/meta';
import { galleryCopy } from '@/i18n/templates';
import {
  FILTERS,
  GALLERY,
  type GalleryFilter,
  matchesFilter,
  parseFilter,
} from '@/templates/catalog';
import { galleryStructuredData } from '@/templates/structuredData';

const route = useRoute();
const { locale } = useLocale();
const copy = computed(() => galleryCopy[locale.value]);
const active = computed(() => parseFilter(route.query.filter));
const shown = computed(() => {
  const filter = active.value;
  return filter === undefined
    ? GALLERY
    : GALLERY.filter((template) => matchesFilter(template, filter));
});
const sampleCount = GALLERY.filter((template) =>
  matchesFilter(template, 'sample')).length;

function label(filter: GalleryFilter): string {
  const name = copy.value.filters[filter];
  return filter === 'sample' ? `${name} ${sampleCount}` : name;
}

function filterLink(filter: GalleryFilter | undefined) {
  return { path: '/templates', query: filter === undefined ? {} : { filter } };
}

useSiteSeo(() => ({
  path: '/templates',
  title: pageTitle(copy.value.seoTitle),
  description: copy.value.lead,
  locale: locale.value,
}));
useHead(computed(() => ({
  script: [{
    key: 'aboutme-structured-data',
    type: 'application/ld+json',
    innerHTML: galleryStructuredData(
      copy.value.seoTitle,
      copy.value.lead,
      GALLERY,
    ),
  }],
})));
</script>

<template>
  <main
    class="mx-auto w-full max-w-[1440px] px-4 py-10 sm:px-10 sm:py-14"
    data-testid="template-gallery"
  >
    <h1 class="text-3xl font-bold tracking-[-0.02em] sm:text-5xl">
      {{ copy.title }}
    </h1>
    <p class="mt-4 max-w-2xl text-base text-muted-foreground">
      {{ copy.lead }}
    </p>
    <nav
      :aria-label="copy.filtersLabel"
      class="gallery-filters -mx-4 mt-8 overflow-x-auto px-4 sm:mx-0 sm:px-0"
    >
      <ul class="flex w-max items-center gap-2">
        <li>
          <NuxtLink
            :aria-current="active === undefined ? 'page' : undefined"
            class="gallery-chip"
            data-filter="all"
            :to="filterLink(undefined)"
          >
            {{ copy.all(GALLERY.length) }}
          </NuxtLink>
        </li>
        <template
          v-for="filter in FILTERS"
          :key="filter"
        >
          <li
            v-if="filter === 'first-job'"
            aria-hidden="true"
            class="mx-1 h-6 w-px bg-border"
          />
          <li>
            <NuxtLink
              :aria-current="active === filter ? 'page' : undefined"
              class="gallery-chip"
              :data-filter="filter"
              :to="filterLink(filter)"
            >
              {{ label(filter) }}
            </NuxtLink>
          </li>
        </template>
      </ul>
    </nav>
    <ul
      v-if="shown.length > 0"
      class="gallery-grid mt-8"
    >
      <li
        v-for="template in shown"
        :key="template.id"
      >
        <TemplateCard
          :illustrative="copy.illustrative"
          :locale="locale"
          :template="template"
        />
      </li>
    </ul>
    <p
      v-else
      class="mt-8 text-muted-foreground"
    >
      {{ copy.noMatch }}
    </p>
  </main>
</template>

<style scoped>
.gallery-filters {
  scrollbar-width: none;
}

.gallery-filters::-webkit-scrollbar {
  display: none;
}

.gallery-chip {
  display: inline-flex;
  align-items: center;
  height: 2.25rem;
  padding: 0 1rem;
  border: 1px solid var(--border);
  border-radius: 9999px;
  background: var(--background);
  color: var(--foreground);
  font-size: 0.875rem;
  white-space: nowrap;
}

.gallery-chip[aria-current="page"] {
  border-color: var(--primary);
  background: var(--primary);
  color: var(--primary-foreground);
}

.gallery-chip:focus-visible {
  outline: 2px solid var(--ring);
  outline-offset: 2px;
}

.gallery-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 24px 14px;
}

@media (width >= 641px) {
  .gallery-grid {
    grid-template-columns: repeat(3, minmax(0, 1fr));
    gap: 40px 28px;
  }
}

@media (width >= 900px) {
  .gallery-grid {
    grid-template-columns: repeat(4, minmax(0, 1fr));
  }
}

@media (width >= 1180px) {
  .gallery-grid {
    grid-template-columns: repeat(5, minmax(0, 1fr));
  }
}
</style>
