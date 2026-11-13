<script setup lang="ts">
import { useMediaQuery } from '@vueuse/core';
import { computed, nextTick, ref, shallowRef, watch } from 'vue';

import EmptyState from '@/components/app/EmptyState.vue';
import StatusBanner from '@/components/app/StatusBanner.vue';
import ShowcaseActiveFilters
  from '@/components/showcase/ShowcaseActiveFilters.vue';
import ShowcaseFilterBar from '@/components/showcase/ShowcaseFilterBar.vue';
import ShowcaseFilters from '@/components/showcase/ShowcaseFilters.vue';
import ShowcaseFilterSheet
  from '@/components/showcase/ShowcaseFilterSheet.vue';
import ShowcaseInvite from '@/components/showcase/ShowcaseInvite.vue';
import ShowcasePager from '@/components/showcase/ShowcasePager.vue';
import ShowcaseTile from '@/components/showcase/ShowcaseTile.vue';
import { Button, buttonVariants } from '@/components/ui/button';
import { Skeleton } from '@/components/ui/skeleton';
import { pageTitle } from '@/i18n/meta';
import { showcaseCopy } from '@/i18n/showcase';
import {
  activeFilterCount,
  parseShowcaseQuery,
  type ShowcaseFilterKind,
  type ShowcaseFilters as Filters,
  showcaseRouteQuery,
} from '@/lib/showcaseQuery';

const route = useRoute();
const router = useRouter();
const { locale } = useLocale();
const { authState } = useAuth();
const { passwordRegistration } = useCapabilities();
const copy = computed(() => showcaseCopy[locale.value]);
const query = computed(() => parseShowcaseQuery(route.query));
const pending = shallowRef<Filters | null>(null);
const filters = computed<Filters>(() => pending.value ?? {
  role: query.value.role,
  lang: query.value.lang,
  template: query.value.template,
});
const { view, listing, retrying, settled, retry } = useShowcase(query);

const results = ref<HTMLElement | null>(null);
const retryButton = ref<{ $el?: HTMLElement } | null>(null);
const rail = ref<HTMLElement | null>(null);
const bar = ref<{ focusFilters(): void } | null>(null);
const SKELETONS = [0, 1, 2, 3, 4, 5];
const WIDE = '(width >= 1024px)';
const activeCount = computed(() => activeFilterCount(filters.value));

const emptyAction = computed(() => {
  if (authState.value === 'authenticated') {
    return { to: '/app/resumes', label: copy.value.emptyActionSignedIn };
  }
  return {
    to: passwordRegistration.value ? '/register' : '/login',
    label: copy.value.emptyAction,
  };
});
const showPager = computed(() => (
  listing.value !== null
  && listing.value.pageCount > 1
  && (view.value === 'list' || view.value === 'no-match')
));

const lastTotal = shallowRef<number | null>(null);
watch(listing, (next) => {
  if (next !== null) lastTotal.value = next.total;
}, { immediate: true });
const countText = computed(() => {
  if (lastTotal.value !== null) return copy.value.countLine(lastTotal.value);
  return view.value === 'failed' ? copy.value.countNoTotal : '';
});
const countHead = computed(() => (
  lastTotal.value === null ? '' : copy.value.countHead(lastTotal.value)
));
const announceKey = ref(0);
const announcePending = ref(false);
let lastLoadedFilters: Filters = query.value;
watch(settled, () => {
  if (view.value === 'failed') return;
  lastLoadedFilters = query.value;
  if (!announcePending.value) return;
  announcePending.value = false;
  announceKey.value += 1;
});
const sheetTotal = computed(() => (
  view.value === 'loading' || listing.value === null
    ? null
    : listing.value.total
));

const sheetOpen = ref(false);
const wide = useMediaQuery(WIDE);
watch(wide, (isWide) => {
  if (isWide) sheetOpen.value = false;
});

function isWide(): boolean {
  return window.matchMedia(WIDE).matches;
}

function focusFilterControl(): void {
  if (!isWide()) {
    bar.value?.focusFilters();
    return;
  }
  rail.value
    ?.querySelector<HTMLElement>(
      '[data-testid="showcase-roles"] [aria-pressed="true"]',
    )
    ?.focus();
}

function onSheetCloseAutoFocus(event: Event): void {
  if (!isWide()) return;
  event.preventDefault();
  focusFilterControl();
}

async function onFilters(next: Filters): Promise<void> {
  announcePending.value = next.role !== lastLoadedFilters.role
    || next.lang !== lastLoadedFilters.lang
    || next.template !== lastLoadedFilters.template;
  pending.value = next;
  try {
    await router.replace({
      path: '/showcase',
      query: showcaseRouteQuery({ ...next, page: 1 }),
    });
  } finally {
    if (pending.value === next) pending.value = null;
  }
}

function onRemove(kind: ShowcaseFilterKind, last: boolean): void {
  const current = filters.value;
  void onFilters({
    role: kind === 'role' ? undefined : current.role,
    lang: kind === 'lang' ? undefined : current.lang,
    template: kind === 'template' ? undefined : current.template,
  });
  if (last) void nextTick(focusFilterControl);
}

function onClearAll(): void {
  void onFilters({});
  void nextTick(focusFilterControl);
}

const focusAfterLoad = ref(false);
function onNavigate(): void {
  focusAfterLoad.value = true;
  const reduced = window.matchMedia('(prefers-reduced-motion: reduce)').matches;
  results.value?.scrollIntoView({
    behavior: reduced ? 'instant' : 'smooth',
    block: 'start',
  });
}
watch(settled, async () => {
  if (!focusAfterLoad.value) return;
  focusAfterLoad.value = false;
  await nextTick();
  if (view.value === 'failed') {
    retryButton.value?.$el?.focus();
  } else {
    results.value
      ?.querySelector<HTMLElement>('[data-showcase-tile]')
      ?.focus();
  }
});

useHead(computed(() => ({
  title: pageTitle(copy.value.title),
  meta: [
    { name: 'description', content: copy.value.description },
    { name: 'robots', content: 'noindex, nofollow' },
  ],
  htmlAttrs: { class: 'filter-bar-scroll-padding' },
})));
</script>

<template>
  <main
    class="showcase-main mx-auto w-full max-w-7xl"
    data-testid="showcase-page"
  >
    <div class="showcase-wrap">
      <header
        class="showcase-header"
        data-testid="showcase-header"
      >
        <h1>{{ copy.title }}</h1>
        <p>{{ copy.lead }}</p>
      </header>
      <ShowcaseFilterBar
        ref="bar"
        v-model:open="sheetOpen"
        :active-count="activeCount"
        :announce-key="announceKey"
        :head="countHead"
        :locale="locale"
        :tail="copy.countTail"
        :text="countText"
      >
        <ShowcaseFilterSheet
          :announce-key="announceKey"
          :filters="filters"
          :locale="locale"
          :status="countText"
          :total="sheetTotal"
          @change="onFilters"
          @close-auto-focus="onSheetCloseAutoFocus"
        />
      </ShowcaseFilterBar>
      <aside
        ref="rail"
        :aria-label="copy.filters"
        class="showcase-rail"
        data-testid="showcase-filter-rail"
      >
        <ShowcaseFilters
          :filters="filters"
          layout="rail"
          :locale="locale"
          @change="onFilters"
        />
      </aside>
      <div
        ref="results"
        class="showcase-results"
        data-testid="showcase-results"
      >
        <ShowcaseActiveFilters
          v-if="activeCount > 0"
          :filters="filters"
          :locale="locale"
          @clear-all="onClearAll"
          @remove="onRemove"
        />
        <ul
          v-if="view === 'loading' || view === 'list'"
          :aria-busy="view === 'loading' ? 'true' : undefined"
          class="showcase-grid"
          :data-state="view"
        >
          <template v-if="view === 'loading'">
            <li
              v-for="index in SKELETONS"
              :key="index"
              aria-hidden="true"
              class="showcase-skeleton"
            >
              <Skeleton class="showcase-skeleton__image" />
              <div class="showcase-skeleton__meta">
                <Skeleton class="h-4 w-2/3" />
              </div>
            </li>
          </template>
          <template v-else-if="listing !== null">
            <ShowcaseTile
              v-for="item in listing.items"
              :key="item.slug"
              :item="item"
              :locale="locale"
            />
            <ShowcaseInvite
              :locale="locale"
              :to="emptyAction.to"
            />
          </template>
        </ul>
        <EmptyState
          v-else-if="view === 'empty'"
          data-state="empty"
          :title="copy.empty"
        >
          <template #action>
            <NuxtLink
              :class="buttonVariants({ variant: 'default' })"
              data-action="showcase-empty-action"
              :to="emptyAction.to"
            >
              {{ emptyAction.label }}
            </NuxtLink>
          </template>
        </EmptyState>
        <p
          v-else-if="view === 'no-match'"
          class="text-[15px] text-muted-foreground"
          data-state="no-match"
        >
          {{ copy.noMatch }}
        </p>
        <StatusBanner
          v-else
          data-state="failed"
          kind="error"
        >
          <p>{{ copy.loadFailed }}</p>
          <Button
            ref="retryButton"
            :aria-disabled="retrying ? 'true' : undefined"
            class="mt-2"
            :class="{ 'pointer-events-none opacity-50': retrying }"
            data-action="showcase-retry"
            size="sm"
            type="button"
            variant="outline"
            @click="retry"
          >
            {{ copy.retry }}
          </Button>
        </StatusBanner>
        <ShowcasePager
          v-if="showPager && listing !== null"
          :filters="filters"
          :locale="locale"
          :page="listing.page"
          :page-count="listing.pageCount"
          @navigate="onNavigate"
        />
      </div>
    </div>
  </main>
</template>

<style scoped>
.showcase-main {
  box-sizing: border-box;
  padding: 20px 16px 40px;
}

.showcase-header h1 {
  margin: 0;
  font-size: 1.75rem;
  font-weight: 700;
  line-height: 1.3;
  letter-spacing: -0.02em;
}

.showcase-header p {
  max-width: 42rem;
  margin: 8px 0 0;
  color: var(--muted-foreground);
  font-size: 0.9375rem;
  line-height: 1.5;
}

.showcase-rail {
  display: none;
}

.showcase-results {
  --showcase-gap: 16px;

  display: grid;
  grid-template-columns: minmax(0, 1fr);
  align-content: start;
  gap: var(--showcase-gap);
  margin-top: 16px;
  container-type: inline-size;
}

.showcase-results :deep(.showcase-pager) {
  margin-top: calc(40px - var(--showcase-gap));
}

.showcase-grid {
  display: grid;
  grid-template-columns: minmax(0, 1fr);
  gap: 16px;
}

.showcase-skeleton {
  display: flex;
  flex-direction: column;
  border: 1px solid var(--border);
  border-radius: var(--radius-dialog);
  background: var(--card);
  box-shadow: var(--shadow-product);
}

.showcase-skeleton__image {
  aspect-ratio: 1200 / 630;
  border-radius: 13px 13px 0 0;
}

.showcase-skeleton__meta {
  padding: 12px 16px 16px;
}

@media (width >= 640px) {
  .showcase-main {
    padding-inline: 32px;
  }
}

@media (width >= 641px) {
  .showcase-main {
    padding-top: 32px;
    padding-bottom: 56px;
  }

  .showcase-header h1 {
    font-size: 2rem;
  }

  .showcase-grid {
    grid-template-columns: repeat(2, minmax(0, 1fr));
    gap: 24px;
  }
}

@media (width >= 1024px) {
  .showcase-main {
    padding-top: 40px;
    padding-bottom: 64px;
  }

  .showcase-wrap {
    display: grid;
    grid-template-columns: 256px minmax(0, 1fr) minmax(18rem, auto);
    grid-template-areas:
      "header header count"
      "rail results results";
    gap: 28px 32px;
  }

  .showcase-header {
    grid-area: header;
  }

  .showcase-header h1 {
    font-size: 2.5rem;
    line-height: 1.25;
  }

  .showcase-header p {
    font-size: 1rem;
    line-height: 1.55;
  }

  .showcase-rail {
    display: block;
    box-sizing: border-box;
    grid-area: rail;
    align-self: start;
    padding: 20px;
    border: 1px solid var(--border);
    border-radius: var(--radius-dialog);
    background: var(--card);
  }

  .showcase-results {
    --showcase-gap: 20px;

    grid-area: results;
    margin-top: 0;
  }

  .showcase-grid {
    gap: 28px;
  }
}

@media (width >= 1024px) {
  @container (width >= 860px) {
    .showcase-grid {
      grid-template-columns: repeat(3, minmax(0, 1fr));
    }
  }
}
</style>
