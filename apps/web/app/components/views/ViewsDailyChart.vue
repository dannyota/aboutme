<script setup lang="ts">
import { computed } from 'vue';
import type { components } from '@/api/generated/openapi';
import type { ViewsDetailCopy } from '@/i18n/views';

type ViewDay = components['schemas']['ViewDay'];
type ViewMonth = components['schemas']['ViewMonth'];

const props = defineProps<{
  readonly days: readonly ViewDay[];
  readonly months: readonly ViewMonth[];
  readonly copy: ViewsDetailCopy;
}>();

// Plain elements and CSS only: each day is a bar whose height is
// proportional to the busiest day in the 90-day window (counting.md,
// "What the owner sees").
const maxReal = computed(() => (
  props.days.reduce((max, day) => Math.max(max, day.real), 0)
));

// True when there is nothing real to chart, so the bars row gives way to
// the "no real views" message instead of a row of invisible bars.
const allZero = computed(() => maxReal.value === 0);

function barHeightPercent(real: number): number {
  if (real === 0 || maxReal.value === 0) return 0;
  return Math.max(2, Math.round((real / maxReal.value) * 100));
}

// First, middle, and last day label the x-axis.
const axisDays = computed(() => {
  const days = props.days;
  if (days.length === 0) return [];
  const picks = [0, Math.floor(days.length / 2), days.length - 1];
  return [...new Set(picks)].map((index) => days[index]?.date ?? '');
});
</script>

<template>
  <section
    aria-labelledby="views-chart-heading"
    class="mt-8 border-t py-8"
  >
    <h2
      id="views-chart-heading"
      class="text-lg font-semibold"
    >
      {{ props.copy.chartHeading }}
    </h2>
    <div
      class="mt-4 rounded-[var(--radius)] border border-border bg-card p-4"
    >
      <p
        v-if="allZero"
        class="py-6 text-center text-sm text-muted-foreground"
        data-testid="views-chart-empty"
      >
        {{ props.copy.chartEmpty }}
      </p>
      <template v-else>
        <div
          aria-hidden="true"
          class="flex h-32 items-end gap-px border-b border-border"
          data-testid="views-daily-bars"
        >
          <div
            v-for="day in props.days"
            :key="day.date"
            class="flex-1 rounded-t bg-primary/70"
            :data-testid="`views-bar-${day.date}`"
            :style="{ height: `${barHeightPercent(day.real)}%` }"
            :title="props.copy.chartDay(day.date, day.real)"
          />
        </div>
        <div
          aria-hidden="true"
          class="mt-1 flex justify-between text-xs text-muted-foreground"
        >
          <span
            v-for="date in axisDays"
            :key="date"
          >{{ props.copy.axisDay(date) }}</span>
        </div>
      </template>
    </div>
    <table class="sr-only">
      <caption>{{ props.copy.chartCaption }}</caption>
      <tbody>
        <tr
          v-for="day in props.days"
          :key="day.date"
        >
          <th scope="row">
            {{ props.copy.fullDay(day.date) }}
          </th>
          <td>{{ props.copy.chartDay(day.date, day.real) }}</td>
        </tr>
      </tbody>
    </table>
  </section>

  <section
    aria-labelledby="views-monthly-heading"
    class="border-t py-8"
  >
    <h2
      id="views-monthly-heading"
      class="text-lg font-semibold"
    >
      {{ props.copy.monthlyHeading }}
    </h2>
    <dl
      class="mt-4 grid grid-cols-2 gap-4 text-sm sm:grid-cols-4"
      data-testid="views-monthly-totals"
    >
      <div
        v-for="month in props.months"
        :key="month.month"
      >
        <dt class="text-muted-foreground">
          {{ props.copy.monthName(month.month) }}
        </dt>
        <dd class="tabular-nums">
          {{ props.copy.realCount(month.real) }}
        </dd>
      </div>
    </dl>
  </section>
</template>
