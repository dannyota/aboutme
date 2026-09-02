<script setup lang="ts">
import { computed, useId } from 'vue';
import { Label } from '@/components/ui/label';
import { cn } from '@/lib/utils';

const props = defineProps<{
  readonly label: string;
  readonly id?: string;
  readonly name?: string;
  readonly hint?: string;
  readonly error?: string;
  readonly required?: boolean;
  readonly class?: string;
}>();
const generated = useId();
const id = computed(() => props.id ?? `field-${generated}`);
const hintId = computed(() => (props.hint ? `${id.value}-hint` : undefined));
const errorId = computed(() => (props.error ? `${id.value}-error` : undefined));
const describedBy = computed(
  () =>
    [hintId.value, errorId.value]
      .filter((value): value is string => value !== undefined)
      .join(' ') || undefined,
);
const invalid = computed(() => (props.error ? true : undefined));
</script>

<template>
  <div
    :class="cn('grid gap-1.5', props.class)"
    :data-field="name"
  >
    <Label
      :for="id"
      :aria-required="required || undefined"
      class="text-sm font-medium"
    >{{ label }}</Label>
    <slot
      :id="id"
      :described-by="describedBy"
      :invalid="invalid"
    />
    <p
      v-if="hint"
      :id="hintId"
      class="text-xs text-muted-foreground"
    >
      {{ hint }}
    </p>
    <p
      v-if="error"
      :id="errorId"
      role="alert"
      :data-error-for="name"
      class="text-xs text-destructive"
    >
      {{ error }}
    </p>
  </div>
</template>
