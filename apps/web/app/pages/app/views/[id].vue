<script setup lang="ts">
import type { components } from '@/api/generated/openapi';
import EmptyState from '@/components/app/EmptyState.vue';
import LoadingState from '@/components/app/LoadingState.vue';
import PageHeader from '@/components/app/PageHeader.vue';
import { Button, buttonVariants } from '@/components/ui/button';
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
const { data, error, pending, refresh } = await useFetch<
  ResumeViewCountsEnvelope
>(
  `/api/v1/views/${encodeURIComponent(id)}`,
  { credentials: 'include', server: false },
);

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
    class="app-page space-y-6"
    data-testid="views-detail-page"
  >
    <LoadingState
      v-if="pending && !data"
      :label="copy.loading"
      testid="views-detail-loading"
    />
    <template v-else-if="notFound">
      <EmptyState
        :title="copy.notFoundTitle"
        :description="copy.notFoundDescription"
        data-testid="views-detail-not-found"
      >
        <template #action>
          <NuxtLink
            :class="buttonVariants({ variant: 'outline' })"
            to="/app/views"
          >
            {{ copy.backToViews }}
          </NuxtLink>
        </template>
      </EmptyState>
    </template>
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
            {{ copy.unavailable }}
          </Button>
        </template>
      </EmptyState>
    </template>
    <template v-else-if="resume">
      <PageHeader :title="resume.title" />
      <p
        class="text-xl font-semibold"
        data-testid="views-headline"
      >
        {{ copy.headline(last90.real, last90.filtered) }}
      </p>
      <p
        class="text-sm text-muted-foreground"
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
        class="text-xs text-muted-foreground"
        data-testid="views-honest-limit"
      >
        {{ copy.honestLimit }}
      </p>
    </template>
  </main>
</template>
