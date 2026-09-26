<script setup lang="ts">
import type { components } from '@/api/generated/openapi';
import EmptyState from '@/components/app/EmptyState.vue';
import LoadingState from '@/components/app/LoadingState.vue';
import PageHeader from '@/components/app/PageHeader.vue';
import StatusBanner from '@/components/app/StatusBanner.vue';
import ViewsResumeCard from '@/components/views/ViewsResumeCard.vue';
import { workspaceTitles } from '@/i18n/meta';
import { viewsIndexCopy } from '@/i18n/views';

type ViewSummaryEnvelope = components['schemas']['ViewSummaryResponse'];

const { locale } = useLocale();
const copy = computed(() => viewsIndexCopy[locale.value]);

useHead({ title: computed(() => workspaceTitles[locale.value].views) });

// The session cookie is only available client-side (no proxy at SSR).
const { data, error, pending } = await useFetch<ViewSummaryEnvelope>(
  '/api/v1/views',
  { credentials: 'include', server: false },
);

const resumes = computed(() => data.value?.data.resumes ?? []);
</script>

<template>
  <main
    class="app-page space-y-6"
    data-testid="views-page"
  >
    <PageHeader :title="copy.title" />
    <LoadingState
      v-if="pending && !data"
      :label="copy.loading"
      testid="views-loading"
    />
    <StatusBanner
      v-else-if="error"
      kind="error"
      testid="views-unavailable"
    >
      {{ copy.unavailable }}
    </StatusBanner>
    <EmptyState
      v-else-if="resumes.length === 0"
      :title="copy.emptyTitle"
      :description="copy.emptyDescription"
      data-testid="views-empty"
    />
    <ul
      v-else
      class="grid gap-4"
      data-testid="views-resume-list"
    >
      <ViewsResumeCard
        v-for="resume in resumes"
        :key="resume.id"
        :resume="resume"
        :copy="copy"
      />
    </ul>
  </main>
</template>
