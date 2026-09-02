<script setup lang="ts">
import { Skeleton } from '@/components/ui/skeleton';
import { cn } from '@/lib/utils';

const props = withDefaults(
  defineProps<{
    readonly label: string;
    readonly lines?: number;
    readonly testid?: string;
    readonly class?: string;
  }>(),
  { lines: 3 },
);
</script>

<template>
  <div
    :class="cn('grid gap-2', props.class)"
    :data-testid="testid"
    role="status"
  >
    <span class="sr-only">{{ label }}</span><Skeleton
      v-for="line in lines"
      :key="line"
      class="h-4 w-full"
    />
  </div>
</template>
