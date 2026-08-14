<script setup lang="ts">
import type { OpaqueCreateOutcome } from '../../../editor/coordinator';

const props = defineProps<{
  open: boolean;
  busy: boolean;
  retained: OpaqueCreateOutcome | null;
}>();

const emit = defineEmits<{
  close: [];
  submit: [title: string, lng: string | null | undefined];
  refresh: [intentId: string];
  abandon: [intentId: string];
}>();

const title = ref('');
const languageMode = ref<'absent' | 'clear' | 'value'>('absent');
const language = ref('');
const titleInput = ref<HTMLInputElement | null>(null);
const returnFocus = ref<HTMLElement | null>(null);

watch(() => props.open, (open) => {
  if (open) {
    returnFocus.value = document.activeElement instanceof HTMLElement
      ? document.activeElement
      : null;
    void nextTick(() => titleInput.value?.focus());
  } else if (returnFocus.value !== null) {
    const target = returnFocus.value;
    returnFocus.value = null;
    void nextTick(() => target.focus());
  }
}, { immediate: true });

function close(): void {
  if (!props.busy) emit('close');
}

function submit(): void {
  const lng = languageMode.value === 'absent'
    ? undefined
    : languageMode.value === 'clear' ? null : language.value;
  emit('submit', title.value, lng);
}
</script>

<template>
  <div
    v-if="open"
    role="dialog"
    aria-modal="true"
    aria-labelledby="create-resume-title"
    aria-describedby="create-resume-description"
    @keydown.esc="close"
  >
    <h2 id="create-resume-title">
      Create resume
    </h2>
    <p id="create-resume-description">
      Create a new private resume.
    </p>
    <template v-if="retained !== null">
      <p role="alert">
        We could not confirm whether this resume was created.
      </p>
      <button
        type="button"
        :disabled="busy"
        @click="$emit('refresh', retained.intent.id)"
      >
        Refresh list
      </button>
      <button
        type="button"
        :disabled="busy"
        @click="$emit('abandon', retained.intent.id)"
      >
        Abandon
      </button>
    </template>
    <form
      v-else
      @submit.prevent="submit"
    >
      <label>
        Title
        <input
          ref="titleInput"
          v-model="title"
          required
          name="title"
          :disabled="busy"
        >
      </label>
      <fieldset :disabled="busy">
        <legend>Language</legend>
        <label><input
          v-model="languageMode"
          type="radio"
          value="absent"
        > Leave unchanged</label>
        <label><input
          v-model="languageMode"
          type="radio"
          value="clear"
        > Clear language</label>
        <label><input
          v-model="languageMode"
          type="radio"
          value="value"
        > Set language</label>
        <label v-if="languageMode === 'value'">
          Language value
          <input
            v-model="language"
            name="lng"
          >
        </label>
      </fieldset>
      <button
        type="submit"
        :disabled="busy"
      >
        Create
      </button>
      <button
        type="button"
        :disabled="busy"
        @click="close"
      >
        Cancel
      </button>
    </form>
  </div>
</template>
