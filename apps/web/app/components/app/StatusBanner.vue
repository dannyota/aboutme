<script setup lang="ts">
import { CheckCircle2, CircleAlert, Info } from '@lucide/vue';
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
const icon = computed(
  () => ({ info: Info, success: CheckCircle2, error: CircleAlert })[props.kind],
);
onMounted(() => {
  if (props.focusOnMount) root.value?.$el?.focus();
});
defineExpose({ focus: (): void => root.value?.$el?.focus() });
</script>

<template>
  <Alert
    ref="root"
    :class="
      cn(kind === 'success' && 'border-positive text-positive', props.class)
    "
    :data-testid="testid"
    :role="kind === 'error' ? 'alert' : 'status'"
    tabindex="-1"
    :variant="kind === 'error' ? 'destructive' : 'default'"
  >
    <component
      :is="icon"
      aria-hidden="true"
      class="size-4"
    />
    <AlertTitle v-if="title">
      {{ title }}
    </AlertTitle>
    <AlertDescription><slot /></AlertDescription>
  </Alert>
</template>
