<script setup lang="ts">
import { computed, nextTick, ref, watch } from 'vue';

import EmptyState from '@/components/app/EmptyState.vue';
import StatusBanner from '@/components/app/StatusBanner.vue';
import ShowcaseFilters from '@/components/showcase/ShowcaseFilters.vue';
import ShowcasePager from '@/components/showcase/ShowcasePager.vue';
import ShowcaseTile from '@/components/showcase/ShowcaseTile.vue';
import { Button, buttonVariants } from '@/components/ui/button';
import { Skeleton } from '@/components/ui/skeleton';
import { pageTitle } from '@/i18n/meta';
import { showcaseCopy } from '@/i18n/showcase';
import {
  parseShowcaseQuery,
  type ShowcaseFilters as Filters,
  showcaseRouteQuery,
} from '@/lib/showcaseQuery';

// The community showcase (docs/design/showcase.md). The server renders the
// heading, lead, filters, and the loading state; the browser then reads the
// listing. The page stores nothing and sends no cookie.
const route = useRoute();
const router = useRouter();
const { locale } = useLocale();
const { authState } = useAuth();
const { passwordRegistration } = useCapabilities();
const copy = computed(() => showcaseCopy[locale.value]);
const query = computed(() => parseShowcaseQuery(route.query));
const filters = computed<Filters>(() => ({
  role: query.value.role,
  lang: query.value.lang,
  template: query.value.template,
}));
const { view, listing, retrying, settled, retry } = useShowcase(query);

const results = ref<HTMLElement | null>(null);
const retryButton = ref<{ $el?: HTMLElement } | null>(null);
const SKELETONS = [0, 1, 2, 3, 4, 5];

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

// A filter change replaces the URL query, drops `page`, and keeps focus on
// the control.
function onFilters(next: Filters): void {
  void router.replace({
    path: '/showcase',
    query: showcaseRouteQuery({ ...next, page: 1 }),
  });
}

// After Previous or Next: scroll the results to the top, then focus the first
// tile link once the page loads, or Retry when it fails.
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
})));
</script>

<template>
  <main
    class="mx-auto w-full max-w-7xl px-4 py-10 sm:px-8 sm:py-14"
    data-testid="showcase-page"
  >
    <header
      class="rounded-[var(--radius-feature)] border border-border
        bg-surface-blue px-5 py-8 sm:px-10 sm:py-12"
      data-testid="showcase-header"
    >
      <h1 class="text-3xl font-bold tracking-[-0.02em] sm:text-5xl">
        {{ copy.title }}
      </h1>
      <p class="mt-4 max-w-[42rem] text-md text-muted-foreground">
        {{ copy.lead }}
      </p>
      <p class="mt-2 text-sm text-muted-foreground">
        {{ copy.orderNote }}
      </p>
    </header>
    <ShowcaseFilters
      :filters="filters"
      :locale="locale"
      @change="onFilters"
    />
    <div
      ref="results"
      class="mt-8"
      data-testid="showcase-results"
    >
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
        role="status"
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
  </main>
</template>

<style scoped>
.showcase-grid {
  display: grid;
  grid-template-columns: minmax(0, 1fr);
  gap: 16px;
}

@media (width >= 641px) {
  .showcase-grid {
    grid-template-columns: repeat(2, minmax(0, 1fr));
    gap: 24px;
  }
}

@media (width >= 1024px) {
  .showcase-grid {
    grid-template-columns: repeat(3, minmax(0, 1fr));
    gap: 32px 28px;
  }
}

.showcase-skeleton {
  display: flex;
  flex-direction: column;
  border: 1px solid var(--border);
  border-radius: var(--radius);
  background: var(--card);
  box-shadow: var(--shadow-product);
}

.showcase-skeleton__image {
  aspect-ratio: 1200 / 630;
  border-radius: 9px 9px 0 0;
}

.showcase-skeleton__meta {
  padding: 12px 16px 16px;
}
</style>
