<script setup lang="ts">
import { computed, useAttrs, useId } from 'vue';
import { Checkbox } from '@/components/ui/checkbox';
import { Label } from '@/components/ui/label';
import { cn } from '@/lib/utils';

const props = defineProps<{
  readonly label: string;
  readonly modelValue: boolean;
  readonly id?: string;
  readonly name?: string;
  readonly description?: string;
  readonly disabled?: boolean;
  readonly class?: string;
}>();
defineOptions({ inheritAttrs: false });
const emit = defineEmits<{ 'update:modelValue': [value: boolean] }>();
const attrs = useAttrs();
const generated = useId();
const fieldId = computed(() => props.id ?? `field-${generated}`);
const descriptionId = computed(() => `${fieldId.value}-description`);

// The description gets an id (docs/design/linkedin-import-ui.md
// Accessibility) so the checkbox's name and description both read; a
// caller's own aria-describedby (an error id, say) still applies, joined
// with the description id when there is one.
const describedBy = computed(() => {
  const own = attrs['aria-describedby'];
  const parts = [
    typeof own === 'string' && own.length > 0 ? own : undefined,
    props.description ? descriptionId.value : undefined,
  ].filter((part): part is string => part !== undefined);
  return parts.length > 0 ? parts.join(' ') : undefined;
});
</script>

<template>
  <div
    :class="cn('flex items-start gap-2', props.class)"
    :data-field="name"
  >
    <Checkbox
      v-bind="$attrs"
      :id="fieldId"
      :aria-describedby="describedBy"
      :name="name"
      :model-value="modelValue"
      :disabled="disabled"
      @update:model-value="(value) => emit('update:modelValue', Boolean(value))"
    />
    <div class="grid gap-1">
      <Label :for="fieldId">{{ label }}</Label>
      <p
        v-if="description"
        :id="descriptionId"
        class="text-sm text-muted-foreground"
      >
        {{ description }}
      </p>
    </div>
  </div>
</template>
