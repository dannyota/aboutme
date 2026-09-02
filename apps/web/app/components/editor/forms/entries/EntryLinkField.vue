<script setup lang="ts">
import type { FieldIntent } from '../fieldIntent';
import { ref } from 'vue';
import TextField from '@/components/app/TextField.vue';

defineProps<{
  readonly label: string;
  readonly modelValue?: string;
}>();
const emit = defineEmits<{ intent: [intent: FieldIntent<string>] }>();
const error = ref('');

function commit(intent: FieldIntent<string>): void {
  if (intent.kind === 'set' && !isLink(intent.value)) {
    error.value = 'Enter an https:// link, or a mailto: or tel: address.';
    return;
  }
  error.value = '';
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
  <TextField
    :label="label"
    :model-value="modelValue"
    type="url"
    :error="error"
    @intent="commit"
  />
</template>
