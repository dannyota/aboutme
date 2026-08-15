<script setup lang="ts">
import type { FieldIntent } from '../fieldIntent';
import OptionalField from '../OptionalField.vue';

defineProps<{
  readonly label: string;
  readonly modelValue?: string;
}>();
const emit = defineEmits<{ intent: [intent: FieldIntent<string>] }>();

function commit(intent: FieldIntent<string>): void {
  if (intent.kind === 'set' && !isLink(intent.value)) {
    return;
  }
  emit('intent', intent);
}

function isLink(value: string): boolean {
  if (
    /\s/.test(value)
    || [...value].some((character) => {
      const code = character.codePointAt(0);
      return code !== undefined && (code < 32 || code === 127);
    })
  ) { return false; }
  if (value.startsWith('https://')) {
    try {
      const parsed = new URL(value);
      return parsed.protocol === 'https:' && parsed.hostname !== '';
    } catch {
      return false;
    }
  }
  return /^(?:mailto|tel):\S+$/.test(value);
}
</script>

<template>
  <OptionalField
    :label="label"
    :model-value="modelValue"
    @intent="commit"
  />
</template>
