<script setup lang="ts">
import { computed, useId } from 'vue';
import type { LocationQuery } from 'vue-router';

import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group';
import type { Locale } from '@/i18n/locale';
import { galleryCopy } from '@/i18n/templates';
import {
  FILTERS,
  GALLERY,
  type GalleryFilter,
  type GalleryRole,
  matchesFilter,
  parseRole,
  ROLES,
  withGalleryFilters,
} from '@/templates/catalog';

const props = defineProps<{
  readonly layout: 'row' | 'sheet';
  readonly locale: Locale;
  readonly role?: GalleryRole;
  readonly filter?: GalleryFilter;
  readonly query: LocationQuery;
  readonly disabled?: boolean;
  readonly styleOnly?: boolean;
}>();
const emit = defineEmits<{
  role: [role: GalleryRole | undefined];
  style: [filter: GalleryFilter | undefined];
}>();
const copy = computed(() => galleryCopy[props.locale]);
const id = useId();
const sampleCount = GALLERY.filter((template) =>
  matchesFilter(template, 'sample'),
).length;
const format = new Set<GalleryFilter>(['sample', 'ats', 'one-page', 'photo']);
const options = [undefined, ...FILTERS];

function target(filter: GalleryFilter | undefined) {
  return {
    path: '/templates',
    query: withGalleryFilters(props.query, props.role, filter),
  };
}

function label(filter: GalleryFilter | undefined): string {
  if (filter === undefined) return copy.value.all(GALLERY.length);
  const text = copy.value.filters[filter];
  return filter === 'sample' ? `${text} ${sampleCount}` : text;
}

function onStyle(event: MouseEvent, filter: GalleryFilter | undefined): void {
  if (
    event.defaultPrevented
    || event.button !== 0
    || event.ctrlKey
    || event.metaKey
    || event.shiftKey
    || event.altKey
  ) return;
  event.preventDefault();
  emit('style', filter);
}

function onRole(value: unknown): void {
  if (value === undefined) {
    emit('role', props.role);
    return;
  }
  const role = value === 'all' ? undefined : parseRole(value);
  if (value !== 'all' && role === undefined) return;
  emit('role', role);
}
</script>

<template>
  <div
    class="library-filters"
    :class="`library-filters--${layout}`"
    data-testid="gallery-filters"
  >
    <section v-if="!styleOnly">
      <h2
        v-if="layout === 'sheet'"
        :id="`${id}-role`"
        class="library-heading"
      >
        {{ copy.roleHeading }}
      </h2>
      <ToggleGroup
        :aria-label="copy.rolesLabel"
        class="library-options"
        data-testid="gallery-roles"
        :disabled="disabled"
        :model-value="role ?? 'all'"
        :spacing="2"
        type="single"
        @update:model-value="onRole"
      >
        <ToggleGroupItem
          v-for="value in ['all', ...ROLES]"
          :key="value"
          class="gallery-chip"
          :data-role="value"
          :value="value"
        >
          {{
            value === 'all' ? copy.allRoles : copy.roles[value as GalleryRole]
          }}
        </ToggleGroupItem>
      </ToggleGroup>
    </section>
    <section>
      <h2
        v-if="layout === 'sheet'"
        :id="`${id}-style`"
        class="library-heading"
      >
        {{ copy.styleHeading }}
      </h2>
      <nav
        :aria-label="copy.stylesLabel"
        data-testid="gallery-styles"
      >
        <ul class="library-options">
          <template
            v-for="value in options"
            :key="value ?? 'all'"
          >
            <li
              v-if="value === 'first-job' && layout === 'sheet'"
              aria-hidden="true"
              class="library-break"
            />
            <li :class="{ 'library-audience-start': value === 'first-job' }">
              <span
                v-if="value === 'first-job'"
                aria-hidden="true"
                class="library-divider"
              />
              <NuxtLink
                v-slot="{ href }"
                custom
                :prefetch="false"
                :to="target(value)"
              >
                <a
                  :aria-current="filter === value ? 'page' : undefined"
                  class="gallery-chip"
                  :data-filter="value ?? 'all'"
                  :data-group="
                    value === undefined
                      ? undefined
                      : format.has(value)
                        ? 'format'
                        : 'audience'
                  "
                  :href="href"
                  @click="onStyle($event, value)"
                >
                  <span
                    v-if="value"
                    aria-hidden="true"
                    class="gallery-chip__dot"
                  />
                  {{ label(value) }}
                </a>
              </NuxtLink>
            </li>
          </template>
        </ul>
      </nav>
    </section>
  </div>
</template>

<style scoped>
.library-filters {
  display: grid;
  gap: 12px;
}
.library-filters--sheet {
  gap: 20px;
}
.library-heading {
  margin-bottom: 8px;
  color: var(--muted-foreground);
  font-size: 14px;
  font-weight: 600;
}
.library-options {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 8px;
}
.library-options > li {
  display: flex;
  align-items: center;
}
.library-divider {
  width: 1px;
  height: 24px;
  margin: 0 4px;
  background: var(--border);
}
.library-break {
  flex-basis: 100%;
  height: 0;
  margin-bottom: -8px;
}
.library-filters--sheet .library-divider {
  display: none;
}
.library-filters :deep(.gallery-chip) {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  height: 36px;
  padding: 0 16px;
  border: 1px solid var(--border);
  border-radius: 9999px;
  background: var(--card);
  color: var(--foreground);
  font-size: 14px;
  font-weight: 500;
  white-space: nowrap;
  transition:
    background-color 150ms,
    border-color 150ms;
}
.library-filters
  :deep(
    .gallery-chip:not([aria-current='page']):not([aria-pressed='true']):hover
  ) {
  background: var(--surface-indigo);
}
.library-filters :deep(.gallery-chip[aria-current='page']),
.library-filters :deep(.gallery-chip[aria-pressed='true']) {
  border-color: var(--primary);
  background: var(--primary);
  color: var(--primary-foreground);
}
.library-filters--sheet :deep(.gallery-chip) {
  height: 40px;
  padding-inline: 14px;
}
.library-filters--sheet :deep(.gallery-chip[aria-current='page']),
.library-filters--sheet :deep(.gallery-chip[aria-pressed='true']) {
  font-weight: 600;
}
.gallery-chip__dot {
  width: 8px;
  height: 8px;
  border-radius: 9999px;
}
[data-group='format'] .gallery-chip__dot {
  background: var(--brand-jade);
}
[data-group='audience'] .gallery-chip__dot {
  background: var(--brand-ochre);
}
[aria-current='page'] .gallery-chip__dot {
  background: currentColor;
}
.library-filters :deep(.gallery-chip:focus-visible) {
  outline: 2px solid var(--ring);
  outline-offset: 2px;
}
</style>
