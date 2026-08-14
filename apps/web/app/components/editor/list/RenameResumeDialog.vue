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
  title.value = item?.title ?? '';
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
  if (props.item !== null) emit('submit', props.item.id, title.value);
}
</script>

<template>
  <div
    v-if="item !== null"
    role="dialog"
    aria-modal="true"
    aria-labelledby="rename-resume-title"
    aria-describedby="rename-resume-description"
    @keydown.esc="close"
  >
    <h2 id="rename-resume-title">
      Rename resume
    </h2>
    <p id="rename-resume-description">
      Enter the new resume title.
    </p>
    <form @submit.prevent="submit">
      <label>Title <input
        ref="input"
        v-model="title"
        required
        :disabled="busy"
      ></label>
      <button
        type="submit"
        :disabled="busy"
      >
        Save
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
