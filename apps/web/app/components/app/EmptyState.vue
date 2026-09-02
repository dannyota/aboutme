<script setup lang="ts">
import { cn } from '@/lib/utils';

const props = defineProps<{
  readonly title: string;
  readonly description?: string;
  readonly class?: string;
}>();
</script>

<template>
  <div
    :class="cn('rounded-lg border border-dashed p-8 text-center', props.class)"
  >
    <p class="font-medium">
      {{ title }}
    </p>
    <p
      v-if="description"
      class="text-muted-foreground"
    >
      {{ description }}
    </p>
    <div class="mt-4">
      <slot name="action" />
    </div>
  </div>
</template>
