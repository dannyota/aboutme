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

const firstDay = computed(() => props.days[0]);
const middleDay = computed(() => (
  props.days[Math.floor(props.days.length / 2)]
));
const lastDay = computed(() => props.days[props.days.length - 1]);
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
          <span>{{ props.copy.axisDay(firstDay.date) }}</span>
          <span>{{ props.copy.axisDay(middleDay.date) }}</span>
          <span>{{ props.copy.axisDay(lastDay.date) }}</span>
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
    class="mt-8 border-t py-8"
  >
    <h2
      id="views-monthly-heading"
      class="text-lg font-semibold"
    >
      {{ props.copy.monthlyHeading }}
    </h2>
    <ul
      class="mt-4 grid grid-cols-2 gap-2 text-sm sm:grid-cols-4"
      data-testid="views-monthly-totals"
    >
      <li
        v-for="month in props.months"
        :key="month.month"
      >
        {{ props.copy.monthLabel(month.month, month.real) }}
      </li>
    </ul>
  </section>
</template>
