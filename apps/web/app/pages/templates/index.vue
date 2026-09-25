<script setup lang="ts">
import { computed } from 'vue';

import TemplateCard from '@/components/templates/TemplateCard.vue';
import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group';
import { pageTitle } from '@/i18n/meta';
import { galleryCopy } from '@/i18n/templates';
import {
  FILTERS,
  GALLERY,
  type GalleryFilter,
  matchesFilter,
  matchesRole,
  parseFilter,
  parseRole,
  ROLES,
  withRole,
} from '@/templates/catalog';
import { galleryStructuredData } from '@/templates/structuredData';

const route = useRoute();
const router = useRouter();
const { locale } = useLocale();
const copy = computed(() => galleryCopy[locale.value]);
const active = computed(() => parseFilter(route.query.filter));
const activeRole = computed(() => parseRole(route.query.role));

function visible(template: (typeof GALLERY)[number]): boolean {
  const filter = active.value;
  const role = activeRole.value;
  if (filter !== undefined && !matchesFilter(template, filter)) return false;
  if (role !== undefined && !matchesRole(template, role)) return false;
  return true;
}

const visibleIds = computed(() =>
  GALLERY.filter(visible).map((template) => template.id));
const sampleCount = GALLERY.filter((template) =>
  matchesFilter(template, 'sample')).length;

function eager(id: string): boolean {
  const rank = visibleIds.value.indexOf(id);
  return rank >= 0 && rank < 2;
}

function label(filter: GalleryFilter): string {
  const name = copy.value.filters[filter];
  return filter === 'sample' ? `${name} ${sampleCount}` : name;
}

function filterLink(filter: GalleryFilter | undefined) {
  return { path: '/templates', query: filter === undefined ? {} : { filter } };
}

// Reka's single toggle group emits undefined when the pressed item is
// pressed again; a role stays selected, like a radio (DESIGN.md, Library).
function onRole(value: unknown): void {
  if (value === undefined) return;
  const role = value === 'all' ? undefined : parseRole(value);
  if (value !== 'all' && role === undefined) return;
  router.replace({ query: withRole(route.query, role) });
}

// The chip dot's color names what the chip narrows: format (what the
// document looks like) or the audience it suits. The dot is decorative;
// the chip text carries the meaning. Role chips carry no dot (DESIGN.md,
// Library).
const FORMAT_FILTERS = new Set<GalleryFilter>([
  'sample',
  'ats',
  'one-page',
  'photo',
]);
function chipGroup(filter: GalleryFilter): 'format' | 'audience' {
  return FORMAT_FILTERS.has(filter) ? 'format' : 'audience';
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
    class="mx-auto w-full max-w-7xl px-4 py-10 sm:px-8 sm:py-14"
    data-testid="template-gallery"
  >
    <header
      class="rounded-[var(--radius-feature)] border border-border
        bg-surface-blue px-5 py-8 sm:px-10 sm:py-12"
      data-testid="gallery-header"
    >
      <h1 class="text-3xl font-bold tracking-[-0.02em] sm:text-5xl">
        {{ copy.title }}
      </h1>
      <p class="mt-4 max-w-2xl text-md text-muted-foreground">
        {{ copy.lead }}
      </p>
    </header>
    <div
      class="gallery-filters -mx-4 mt-8 overflow-x-auto px-4 sm:mx-0 sm:px-0"
    >
      <ToggleGroup
        :aria-label="copy.rolesLabel"
        class="flex w-max items-center gap-2"
        data-testid="gallery-roles"
        :model-value="activeRole ?? 'all'"
        :spacing="2"
        type="single"
        @update:model-value="onRole"
      >
        <ToggleGroupItem
          class="gallery-chip"
          data-role="all"
          value="all"
        >
          {{ copy.allRoles }}
        </ToggleGroupItem>
        <ToggleGroupItem
          v-for="role in ROLES"
          :key="role"
          class="gallery-chip"
          :data-role="role"
          :value="role"
        >
          {{ copy.roles[role] }}
        </ToggleGroupItem>
      </ToggleGroup>
    </div>
    <nav
      :aria-label="copy.filtersLabel"
      class="gallery-filters -mx-4 mt-3 overflow-x-auto px-4 sm:mx-0 sm:px-0"
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
              :data-group="chipGroup(filter)"
              :to="filterLink(filter)"
            >
              <span
                aria-hidden="true"
                class="gallery-chip__dot"
              />
              {{ label(filter) }}
            </NuxtLink>
          </li>
        </template>
      </ul>
    </nav>
    <ul class="gallery-grid mt-8">
      <li
        v-for="template in GALLERY"
        :key="template.id"
        :hidden="!visible(template)"
      >
        <TemplateCard
          :eager="eager(template.id)"
          :illustrative="copy.illustrative"
          :locale="locale"
          :page-image-alt="copy.pageImageAlt"
          :template="template"
        />
      </li>
    </ul>
    <p
      v-if="visibleIds.length === 0"
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
  gap: 0.5rem;
  height: 2.25rem;
  padding: 0 1rem;
  border: 1px solid var(--border);
  border-radius: 9999px;
  background: var(--card);
  color: var(--foreground);
  font-size: 0.875rem;
  font-weight: 500;
  white-space: nowrap;
  transition: background-color 150ms, border-color 150ms;
}

.gallery-chip:not([aria-current="page"]):not([aria-pressed="true"]):hover {
  background: var(--surface-indigo);
}

.gallery-chip[aria-current="page"],
.gallery-chip[aria-pressed="true"] {
  border-color: var(--primary);
  background: var(--primary);
  color: var(--primary-foreground);
}

.gallery-chip__dot {
  width: 0.5rem;
  height: 0.5rem;
  border-radius: 9999px;
}

[data-group="format"] .gallery-chip__dot {
  background: var(--brand-cyan);
}

[data-group="audience"] .gallery-chip__dot {
  background: var(--brand-purple);
}

[aria-current="page"] .gallery-chip__dot {
  background: currentColor;
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
