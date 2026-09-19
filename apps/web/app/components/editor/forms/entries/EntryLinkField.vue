<script setup lang="ts">
import type { FieldIntent } from '../fieldIntent';
import { computed, ref } from 'vue';
import { editorFieldsCopy } from '@/i18n/editor-fields';
import TextField from '@/components/app/TextField.vue';

defineProps<{
  readonly label: string;
  readonly modelValue?: string;
}>();
const emit = defineEmits<{ intent: [intent: FieldIntent<string>] }>();
const error = ref(false);
const { locale } = useLocale();
const copy = computed(() => editorFieldsCopy[locale.value].links);

function commit(intent: FieldIntent<string>): void {
  if (intent.kind === 'set' && !isLink(intent.value)) {
    error.value = true;
    return;
  }
  error.value = false;
  emit('intent', intent);
}

function isLink(value: string): boolean {
  if (
    /\s/.test(value)
    || [...value].some((character) => {
      const code = character.codePointAt(0);
      return code !== undefined && (code < 32 || code === 127);
    })
  ) {
    return false;
  }
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
    :error="error ? copy.invalid : undefined"
    @intent="commit"
  />
</template>
