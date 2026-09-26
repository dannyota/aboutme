<script setup lang="ts">
import type { components } from '@/api/generated/openapi';
import type { ViewsIndexCopy } from '@/i18n/views';

type ViewSummary = components['schemas']['ViewSummary'];

const props = defineProps<{
  readonly resume: ViewSummary;
  readonly copy: ViewsIndexCopy;
}>();
</script>

<template>
  <li
    class="rounded-lg border p-4"
    :data-testid="`views-resume-${props.resume.id}`"
  >
    <NuxtLink
      class="flex items-start justify-between gap-4"
      :to="`/app/views/${props.resume.id}`"
    >
      <div>
        <p class="font-medium">
          {{ props.resume.title }}
        </p>
        <p class="text-sm text-muted-foreground">
          {{ props.resume.live ? props.copy.live : props.copy.draft }}
        </p>
      </div>
    </NuxtLink>
    <dl class="mt-3 grid grid-cols-1 gap-2 text-sm sm:grid-cols-3">
      <div>
        <dt class="text-muted-foreground">
          {{ props.copy.last7 }}
        </dt>
        <dd>
          {{ props.copy.realFiltered(
            props.resume.last7.real, props.resume.last7.filtered,
          ) }}
        </dd>
      </div>
      <div>
        <dt class="text-muted-foreground">
          {{ props.copy.last30 }}
        </dt>
        <dd>
          {{ props.copy.realFiltered(
            props.resume.last30.real, props.resume.last30.filtered,
          ) }}
        </dd>
      </div>
      <div>
        <dt class="text-muted-foreground">
          {{ props.copy.last90 }}
        </dt>
        <dd>
          {{ props.copy.realFiltered(
            props.resume.last90.real, props.resume.last90.filtered,
          ) }}
        </dd>
      </div>
    </dl>
  </li>
</template>
