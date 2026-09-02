<script setup lang="ts">
import { computed, useId } from 'vue';
import { Switch } from '@/components/ui/switch';
import { Label } from '@/components/ui/label';
import { cn } from '@/lib/utils';

const props = defineProps<{
  readonly label: string;
  readonly modelValue: boolean;
  readonly id?: string;
  readonly description?: string;
  readonly disabled?: boolean;
  readonly class?: string;
}>();
defineOptions({ inheritAttrs: false });
const emit = defineEmits<{ 'update:modelValue': [value: boolean] }>();
const generated = useId();
const fieldId = computed(() => props.id ?? `field-${generated}`);
</script>

<template>
  <div :class="cn('flex items-start gap-2', props.class)">
    <Switch
      v-bind="$attrs"
      :id="fieldId"
      :model-value="modelValue"
      :disabled="disabled"
      @update:model-value="(value) => emit('update:modelValue', Boolean(value))"
    />
    <div class="grid gap-1">
      <Label :for="fieldId">{{ label }}</Label>
      <p
        v-if="description"
        class="text-sm text-muted-foreground"
      >
        {{ description }}
      </p>
    </div>
  </div>
</template>
