<script setup lang="ts">
import { ChevronLeft } from '@lucide/vue';
import type { components } from '@/api/generated/openapi';
import EmptyState from '@/components/app/EmptyState.vue';
import LoadingState from '@/components/app/LoadingState.vue';
import PageHeader from '@/components/app/PageHeader.vue';
import { Button } from '@/components/ui/button';
import ViewsDailyChart from '@/components/views/ViewsDailyChart.vue';
import ViewsFilteredBreakdown
  from '@/components/views/ViewsFilteredBreakdown.vue';
import ViewsLinkPreviews from '@/components/views/ViewsLinkPreviews.vue';
import { workspaceTitles } from '@/i18n/meta';
import { viewsDetailCopy } from '@/i18n/views';

type ResumeViewCountsEnvelope
  = components['schemas']['ResumeViewCountsResponse'];

const route = useRoute();
const id = String(route.params.id);
const { locale } = useLocale();
const copy = computed(() => viewsDetailCopy[locale.value]);

useHead({ title: computed(() => workspaceTitles[locale.value].views) });

// The session cookie is only available client-side (no proxy at SSR); a
// resume owned by another account answers exactly like a missing one
// (docs/api/openapi.yaml, getResumeViewCounts).
const { data, error, status, refresh } = await useFetch<
  ResumeViewCountsEnvelope
>(
  `/api/v1/views/${encodeURIComponent(id)}`,
  { credentials: 'include', server: false },
);

// server:false leaves `status` 'idle' during SSR, but hydration starts the
// fetch (setting status 'pending') before the client's first render;
// treating both as loading keeps the SSR and hydration renders identical.
const loading = computed(() => (
  status.value === 'idle' || status.value === 'pending'
));

const notFound = computed(() => (
  (error.value as { statusCode?: number } | null)?.statusCode === 404
));
const resume = computed(() => data.value?.data);

const last90 = computed(() => {
  const days = resume.value?.days ?? [];
  const real = days.reduce((sum, day) => sum + day.real, 0);
  const filtered = days.reduce(
    (sum, day) => sum + day.bot + day.datacenter + day.anomaly + day.invalid
      + day.crawler,
    0,
  );
  return { real, filtered };
});
</script>

<template>
  <main
    class="mx-auto w-full max-w-3xl px-6 py-10"
    data-testid="views-detail-page"
  >
    <NuxtLink
      class="mb-6 inline-flex items-center gap-1 rounded-[var(--radius)]
        text-sm text-muted-foreground hover:text-foreground
        focus-visible:outline-none focus-visible:ring-2
        focus-visible:ring-ring focus-visible:ring-offset-2"
      to="/app/views"
    >
      <ChevronLeft
        aria-hidden="true"
        :size="16"
      />
      {{ copy.backToViews }}
    </NuxtLink>
    <LoadingState
      v-if="loading"
      :label="copy.loading"
      testid="views-detail-loading"
    />
    <EmptyState
      v-else-if="notFound"
      :title="copy.notFoundTitle"
      :description="copy.notFoundDescription"
      data-testid="views-detail-not-found"
    />
    <template v-else-if="error">
      <EmptyState
        :title="copy.unavailable"
        data-testid="views-detail-unavailable"
      >
        <template #action>
          <Button
            type="button"
            @click="refresh"
          >
            {{ copy.retry }}
          </Button>
        </template>
      </EmptyState>
    </template>
    <template v-else-if="resume">
      <PageHeader :title="resume.title" />
      <p
        class="mt-6 flex flex-wrap items-baseline gap-x-2"
        data-testid="views-headline"
      >
        <span class="text-4xl font-bold tabular-nums tracking-tight">{{
          copy.headlineCount(last90.real)
        }}</span>
        <!-- The leading space keeps the two spans' textContent readable
             as one sentence; whitespace-only text between elements is
             stripped, so the space has to live inside this span. -->
        <span class="text-base text-muted-foreground">{{
          ' ' + copy.headlineRest(last90.real, last90.filtered)
        }}</span>
      </p>
      <p
        class="mt-2 max-w-prose text-sm text-muted-foreground"
        data-testid="views-definition"
      >
        {{ copy.definition }}
      </p>
      <ViewsDailyChart
        :days="resume.days"
        :months="resume.months"
        :copy="copy"
      />
      <ViewsFilteredBreakdown
        :days="resume.days"
        :copy="copy"
      />
      <ViewsLinkPreviews
        :previews="resume.previews"
        :copy="copy"
      />
      <p
        class="pt-2 text-xs text-muted-foreground"
        data-testid="views-honest-limit"
      >
        {{ copy.honestLimit }}
      </p>
    </template>
  </main>
</template>
