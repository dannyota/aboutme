<script setup lang="ts">
import { computed } from 'vue';
import type { components } from '@/api/generated/openapi';
import type { ViewsDetailCopy, ViewsFilteredKey } from '@/i18n/views';

type ViewDay = components['schemas']['ViewDay'];

const props = defineProps<{
  readonly days: readonly ViewDay[];
  readonly copy: ViewsDetailCopy;
}>();

const FILTERED_KEYS: readonly ViewsFilteredKey[] = [
  'bot', 'datacenter', 'anomaly', 'invalid', 'crawler',
];

const totals = computed(() => {
  const sums: Record<ViewsFilteredKey, number> = {
    bot: 0, datacenter: 0, anomaly: 0, invalid: 0, crawler: 0,
  };
  for (const day of props.days) {
    sums.bot += day.bot;
    sums.datacenter += day.datacenter;
    sums.anomaly += day.anomaly;
    sums.invalid += day.invalid;
    sums.crawler += day.crawler;
  }
  return sums;
});
</script>

<template>
  <section
    aria-labelledby="views-filtered-heading"
    class="border-t py-8"
  >
    <h2
      id="views-filtered-heading"
      class="text-lg font-semibold"
    >
      {{ props.copy.filteredHeading }}
    </h2>
    <dl
      class="mt-4 grid grid-cols-2 gap-4 text-sm sm:grid-cols-5"
      data-testid="views-filtered-breakdown"
    >
      <div
        v-for="key in FILTERED_KEYS"
        :key="key"
      >
        <!-- Balanced wrapping keeps a long Vietnamese label such as
             "Không vượt qua kiểm tra" from leaving one word alone on the
             second line in the narrow five-column grid. -->
        <dt class="text-balance text-muted-foreground">
          {{ props.copy.filteredLabels[key] }}
        </dt>
        <dd class="text-lg font-semibold tabular-nums">
          {{ props.copy.count(totals[key]) }}
        </dd>
      </div>
    </dl>
  </section>
</template>
