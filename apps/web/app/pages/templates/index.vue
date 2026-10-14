<script setup lang="ts">
import { useMediaQuery } from '@vueuse/core';
import {
  computed,
  createStaticVNode,
  defineComponent,
  h,
  nextTick,
  onBeforeUnmount,
  onMounted,
  ref,
  shallowRef,
  watch,
} from 'vue';
import type { LocationQuery } from 'vue-router';

import ActiveFilters from '@/components/app/ActiveFilters.vue';
import FilterBar from '@/components/app/FilterBar.vue';
import FilterSheet from '@/components/app/FilterSheet.vue';
import LibraryFilters from '@/components/templates/LibraryFilters.vue';
import TemplateCard from '@/components/templates/TemplateCard.vue';
import { pageTitle } from '@/i18n/meta';
import { galleryCopy } from '@/i18n/templates';
import {
  GALLERY,
  type GalleryFilter,
  type GalleryRole,
  matchesFilter,
  matchesRole,
  parseFilter,
  parseRole,
  withGalleryFilters,
} from '@/templates/catalog';
import { galleryStructuredData } from '@/templates/structuredData';

const NoScriptFallback = defineComponent({
  name: 'NoScriptFallback',
  setup(_props, { slots }) {
    // With scripts enabled, noscript holds raw text, not child elements.
    // Adopt the server node without hydrating its fallback children.
    return () => import.meta.server
      ? h('noscript', null, slots.default?.())
      : createStaticVNode('<noscript></noscript>', 1);
  },
});

const route = useRoute();
const router = useRouter();
const { locale } = useLocale();
const copy = computed(() => galleryCopy[locale.value]);
type Pair = {
  role: GalleryRole | undefined;
  filter: GalleryFilter | undefined;
};
function pair(query: LocationQuery): Pair {
  return { role: parseRole(query.role), filter: parseFilter(query.filter) };
}
function same(a: Pair, b: Pair): boolean {
  return a.role === b.role && a.filter === b.filter;
}
const committed = shallowRef(pair(route.query));
let intended = committed.value;
let announced = committed.value;
let navigation = 0;
let pending = false;
const removeRouteHook = router.afterEach((to, _from, failure) => {
  if (!pending || failure) return;
  navigation += 1;
  pending = false;
  intended = pair(to.query);
  commit(intended);
});
const announceKey = ref(0);
const hydrated = ref(false);
const sheetOpen = ref(false);
const desktop = ref<HTMLElement | null>(null);
const phone = ref<HTMLElement | null>(null);
const bar = ref<{ focusFilters(): void } | null>(null);
const wide = useMediaQuery('(width >= 1024px)');
let focusVersion = 0;
let initiator: HTMLElement | null = null;
function onFocus(): void {
  focusVersion += 1;
}
onMounted(() => {
  hydrated.value = true;
  document.addEventListener('focusin', onFocus);
});
onBeforeUnmount(() => {
  document.removeEventListener('focusin', onFocus);
  removeRouteHook();
});

function commit(next: Pair): void {
  committed.value = next;
  if (!same(next, announced)) {
    announced = next;
    announceKey.value += 1;
  }
}
watch(
  () => route.query,
  (query) => {
    if (pending) return;
    navigation += 1;
    intended = pair(query);
    commit(intended);
  },
);

function visible(template: (typeof GALLERY)[number]): boolean {
  const { role, filter } = committed.value;
  return (
    (filter === undefined || matchesFilter(template, filter))
    && (role === undefined || matchesRole(template, role))
  );
}
const visibleIds = computed(() => GALLERY.filter(visible).map(({ id }) => id));
function eager(id: string): boolean {
  const rank = visibleIds.value.indexOf(id);
  return rank >= 0 && rank < 2;
}
const count = computed(() => copy.value.count(visibleIds.value.length));
const chips = computed(() => {
  const { role, filter } = committed.value;
  return [
    ...(role === undefined
      ? []
      : [{ kind: 'role', label: copy.value.roles[role] }]),
    ...(filter === undefined
      ? []
      : [{ kind: 'style', label: copy.value.filters[filter] }]),
  ];
});
const clearTarget = computed(() => ({
  path: '/templates',
  query: withGalleryFilters(route.query, undefined, undefined),
}));

function focusDesktop(): void {
  desktop.value
    ?.querySelector<HTMLElement>('[data-role][aria-pressed="true"]')
    ?.focus();
}
function focusFilters(): void {
  bar.value?.focusFilters();
}
watch(wide, async (isWide) => {
  const active = document.activeElement;
  const repair = isWide
    ? sheetOpen.value
    || phone.value?.contains(active)
    || active?.matches('[data-action="gallery-filters-open"]')
    : desktop.value?.contains(active);
  if (isWide) sheetOpen.value = false;
  if (!repair) return;
  await nextTick();
  if (isWide) focusDesktop();
  else focusFilters();
});
function onClose(event: Event): void {
  if (!wide.value) return;
  event.preventDefault();
  focusDesktop();
}

type Recovery = {
  control: HTMLElement | null;
  destination: Element | null;
  version: number;
};
function recover(snapshot: Recovery | undefined): void {
  if (!snapshot) return;
  const { control, destination, version } = snapshot;
  if (
    control?.isConnected
    && !control.matches(':disabled')
    && control.getClientRects().length > 0
    && document.activeElement === destination
    && focusVersion === version
  ) control.focus();
}
async function change(next: Pair, recovery?: Recovery): Promise<void> {
  if (same(next, intended)) return;
  intended = next;
  const token = ++navigation;
  pending = true;
  const target = {
    path: '/templates',
    query: withGalleryFilters(route.query, next.role, next.filter),
  };
  try {
    const failure = await router.replace(target);
    if (token !== navigation) return;
    if (failure) recover(recovery);
    else commit(next);
  } catch {
    if (token === navigation) recover(recovery);
  } finally {
    if (token === navigation) {
      pending = false;
      intended = committed.value;
    }
  }
}
function onRole(role: GalleryRole | undefined): void {
  void change({ ...intended, role });
}
function onStyle(filter: GalleryFilter | undefined): void {
  void change({ ...intended, filter });
}
function capture(event: Event): void {
  initiator = (event.target as HTMLElement).closest<HTMLElement>('button');
}
async function remove(kind: string, last: boolean): Promise<void> {
  const control = initiator;
  const next = {
    ...intended,
    [kind === 'role' ? 'role' : 'filter']: undefined,
  };
  if (last) focusFilters();
  const recovery = {
    control,
    destination: document.activeElement,
    version: focusVersion,
  };
  await change(next, recovery);
}
function clearAll(): void {
  const recovery = {
    control: initiator,
    destination: document.activeElement,
    version: focusVersion,
  };
  focusFilters();
  recovery.destination = document.activeElement;
  recovery.version = focusVersion;
  void change({ role: undefined, filter: undefined }, recovery);
}
useSiteSeo(() => ({
  path: '/templates',
  title: pageTitle(copy.value.seoTitle),
  description: copy.value.lead,
  locale: locale.value,
}));
useHead(
  computed(() => ({
    htmlAttrs: { class: 'filter-bar-scroll-padding' },
    script: [
      {
        key: 'aboutme-structured-data',
        type: 'application/ld+json',
        innerHTML: galleryStructuredData(
          copy.value.seoTitle,
          copy.value.lead,
          GALLERY,
        ),
      },
    ],
  })),
);
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
      ref="desktop"
      class="gallery-desktop mt-8"
    >
      <LibraryFilters
        :disabled="!hydrated"
        :filter="committed.filter"
        layout="row"
        :locale="locale"
        :query="route.query"
        :role="committed.role"
        @role="onRole"
        @style="onStyle"
      />
    </div>
    <FilterBar
      ref="bar"
      :active-count="chips.length"
      :announce-key="announceKey"
      :copy="copy.filterControls"
      :disabled="!hydrated"
      layout="library"
      :open="sheetOpen"
      prefix="gallery"
      :text="count"
      @update:open="sheetOpen = $event"
    >
      <FilterSheet
        v-if="sheetOpen"
        :announce-key="announceKey"
        :copy="copy.filterControls"
        prefix="gallery"
        :results-label="copy.showResults(visibleIds.length)"
        :status="count"
        @clear="change({ role: undefined, filter: undefined })"
        @close-auto-focus="onClose"
      >
        <LibraryFilters
          :filter="committed.filter"
          layout="sheet"
          :locale="locale"
          :query="route.query"
          :role="committed.role"
          @role="onRole"
          @style="onStyle"
        />
      </FilterSheet>
    </FilterBar>
    <div
      v-if="chips.length"
      ref="phone"
      class="gallery-phone mt-4"
      @click.capture="capture"
    >
      <ActiveFilters
        :chips="chips"
        :copy="copy.filterControls"
        :disabled="!hydrated"
        prefix="gallery"
        @remove="remove"
        @clear-all="clearAll"
      />
    </div>
    <NoScriptFallback>
      <div class="gallery-fallback mt-4">
        <LibraryFilters
          :filter="committed.filter"
          layout="row"
          :locale="locale"
          :query="route.query"
          :role="committed.role"
          style-only
        />
      </div>
      <NuxtLink
        v-if="chips.length"
        class="gallery-fallback-clear"
        :prefetch="false"
        :to="clearTarget"
      >
        {{ copy.filterControls.clearAll }}
      </NuxtLink>
    </NoScriptFallback>
    <ul
      v-show="visibleIds.length"
      class="gallery-grid mt-4"
    >
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
      class="mt-4 text-base text-muted-foreground"
    >
      {{ copy.noMatch }}
    </p>
  </main>
</template>

<style scoped>
.gallery-desktop {
  display: none;
}
.gallery-fallback-clear {
  display: block;
  margin-top: 12px;
  color: var(--link);
  text-decoration: underline;
}
@media (width >= 1024px) {
  .gallery-desktop {
    display: block;
  }
  .gallery-phone,
  .gallery-fallback {
    display: none;
  }
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
