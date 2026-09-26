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

function barHeightPercent(real: number): number {
  if (maxReal.value === 0) return 0;
  return Math.max(2, Math.round((real / maxReal.value) * 100));
}
</script>

<template>
  <section aria-labelledby="views-chart-heading">
    <h2
      id="views-chart-heading"
      class="text-lg font-semibold"
    >
      {{ props.copy.chartHeading }}
    </h2>
    <div
      aria-hidden="true"
      class="mt-3 flex h-32 items-end gap-px"
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
    <table class="sr-only">
      <caption>{{ props.copy.chartCaption }}</caption>
      <tbody>
        <tr
          v-for="day in props.days"
          :key="day.date"
        >
          <th scope="row">
            {{ day.date }}
          </th>
          <td>{{ props.copy.chartDay(day.date, day.real) }}</td>
        </tr>
      </tbody>
    </table>

    <h3 class="mt-6 font-semibold">
      {{ props.copy.monthlyHeading }}
    </h3>
    <ul
      class="mt-2 grid grid-cols-2 gap-2 text-sm sm:grid-cols-4"
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
