<script setup lang="ts">
import type { components } from '@/api/generated/openapi';
import StateMark from '@/components/app/StateMark.vue';
import type { ViewsIndexCopy } from '@/i18n/views';

type ViewSummary = components['schemas']['ViewSummary'];

const props = defineProps<{
  readonly resume: ViewSummary;
  readonly copy: ViewsIndexCopy;
}>();
</script>

<template>
  <li
    class="relative flex min-h-48 flex-col rounded-[var(--radius)]
      border border-border bg-card shadow-[var(--shadow-product)]
      transition-transform duration-200 ease-out hover:-translate-y-1
      motion-reduce:transition-none motion-reduce:hover:translate-y-0"
    :data-testid="`views-resume-${props.resume.id}`"
  >
    <NuxtLink
      class="block p-6 pb-3 after:absolute after:inset-0"
      :to="`/app/views/${props.resume.id}`"
    >
      <span class="block truncate pr-4 text-lg font-semibold">
        {{ props.resume.title }}
      </span>
      <dl class="mt-3 grid grid-cols-1 gap-3 text-sm sm:grid-cols-3">
        <div>
          <dt class="text-xs text-muted-foreground">
            {{ props.copy.last7 }}
          </dt>
          <dd class="tabular-nums">
            {{ props.copy.realFiltered(
              props.resume.last7.real, props.resume.last7.filtered,
            ) }}
          </dd>
        </div>
        <div>
          <dt class="text-xs text-muted-foreground">
            {{ props.copy.last30 }}
          </dt>
          <dd class="tabular-nums">
            {{ props.copy.realFiltered(
              props.resume.last30.real, props.resume.last30.filtered,
            ) }}
          </dd>
        </div>
        <div>
          <dt class="text-xs text-muted-foreground">
            {{ props.copy.last90 }}
          </dt>
          <dd class="tabular-nums">
            {{ props.copy.realFiltered(
              props.resume.last90.real, props.resume.last90.filtered,
            ) }}
          </dd>
        </div>
      </dl>
    </NuxtLink>
    <span
      class="relative z-10 mt-auto block px-6 pb-5"
      :class="{
        'pointer-events-none': !props.resume.live || !props.resume.slug,
      }"
    >
      <StateMark
        v-if="props.resume.live && props.resume.slug"
        state="public"
        :link="`/${props.resume.slug}`"
      />
      <StateMark
        v-else
        state="draft"
      />
    </span>
  </li>
</template>
