<script setup lang="ts">
import type { ResumeSummary } from '../../../editor/resumeApi';

const props = defineProps<{
  item: ResumeSummary | null;
  busy: boolean;
}>();

const emit = defineEmits<{ close: []; submit: [id: string, title: string] }>();
const title = ref('');
const input = ref<HTMLInputElement | null>(null);
const returnFocus = ref<HTMLElement | null>(null);

watch(() => props.item, (item) => {
  title.value = '';
  if (item !== null) {
    returnFocus.value = document.activeElement instanceof HTMLElement
      ? document.activeElement
      : null;
    void nextTick(() => input.value?.focus());
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
  if (props.item !== null && title.value === props.item.title) {
    emit('submit', props.item.id, title.value);
  }
}
</script>

<template>
  <div
    v-if="item !== null"
    role="dialog"
    aria-modal="true"
    aria-labelledby="delete-resume-title"
    aria-describedby="delete-resume-description"
    @keydown.esc="close"
  >
    <h2 id="delete-resume-title">
      Delete resume
    </h2>
    <p id="delete-resume-description">
      This permanently deletes the resume. Type its title to confirm.
    </p>
    <form @submit.prevent="submit">
      <label>Current title <input
        ref="input"
        v-model="title"
        :disabled="busy"
      ></label>
      <p
        v-if="title !== item.title"
        role="status"
      >
        Enter the current title to enable deletion.
      </p>
      <button
        type="submit"
        :disabled="busy || title !== item.title"
      >
        Delete
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
