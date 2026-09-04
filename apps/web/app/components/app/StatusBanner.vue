<script setup lang="ts">
import { CircleAlert, Info } from '@lucide/vue';
import { computed, onMounted, ref } from 'vue';
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert';
import { cn } from '@/lib/utils';

const props = defineProps<{
  readonly kind: 'info' | 'success' | 'error';
  readonly title?: string;
  readonly testid?: string;
  readonly focusOnMount?: boolean;
  readonly class?: string;
}>();
const root = ref<{ $el?: HTMLElement } | null>(null);
const icon = computed(() => (props.kind === 'error' ? CircleAlert : Info));
onMounted(() => {
  if (props.focusOnMount) root.value?.$el?.focus();
});
defineExpose({ focus: (): void => root.value?.$el?.focus() });
</script>

<template>
  <Alert
    ref="root"
    :class="
      cn(kind === 'success' && 'border-border text-foreground', props.class)
    "
    :data-testid="testid"
    :role="kind === 'error' ? 'alert' : 'status'"
    tabindex="-1"
    :variant="kind === 'error' ? 'destructive' : 'default'"
  >
    <svg
      v-if="kind === 'success'"
      aria-hidden="true"
      class="size-4"
      data-status-glyph="success"
      fill="none"
      viewBox="0 0 14 14"
    >
      <path
        d="m2 10.75 1.1-3.3L9.8.75l2.45 2.45-6.7 6.7L2 10.75Z"
        stroke="currentColor"
        stroke-linejoin="round"
        stroke-width="1.25"
      />
      <path
        d="m7.75 10.25 1.4 1.4 2.85-3.1"
        stroke="currentColor"
        stroke-linecap="round"
        stroke-linejoin="round"
        stroke-width="1.25"
      />
    </svg>
    <component
      :is="icon"
      v-else
      aria-hidden="true"
      class="size-4"
    />
    <AlertTitle v-if="title">
      {{ title }}
    </AlertTitle>
    <AlertDescription><slot /></AlertDescription>
  </Alert>
</template>
