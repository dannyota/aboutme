<script setup lang="ts">
import { cn } from '@/lib/utils';

const props = withDefaults(
  defineProps<{
    readonly title: string;
    readonly description?: string;
    readonly titleId?: string;
    readonly class?: string;
  }>(),
  { titleId: 'page-title' },
);
</script>

<template>
  <div :class="cn('flex items-start justify-between gap-4', props.class)">
    <div>
      <h1
        :id="titleId"
        class="text-2xl font-semibold tracking-tight"
      >
        {{ title }}
      </h1>
      <p
        v-if="description"
        class="text-muted-foreground"
      >
        {{ description }}
      </p>
    </div>
    <div class="ml-auto flex items-center gap-2">
      <slot name="actions" />
    </div>
  </div>
</template>
