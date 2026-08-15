<script setup lang="ts">
import { nextTick, ref, watch } from 'vue';

import type { ServerValidationIssue } from '../../editor/attempt';

const props = defineProps<{
  readonly issues: readonly ServerValidationIssue[];
}>();
const emit = defineEmits<{
  focusIssue: [path: string];
}>();
const summary = ref<HTMLElement | null>(null);

watch(
  () => props.issues.map(({ path, code }) => `${path}:${code}`).join('|'),
  async (next, previous) => {
    if (next === '' || next === previous) return;
    await nextTick();
    summary.value?.focus();
  },
);

function safeText(code: string): string {
  switch (code) {
    case 'maxLength':
    case 'maxItems':
    case 'maximum':
      return 'This value is over the allowed limit.';
    case 'required':
      return 'Add the required value.';
    case 'format':
    case 'pattern':
    case 'date-range-order':
      return 'Check this value and try again.';
    default:
      return 'This value needs attention.';
  }
}
</script>

<template>
  <section
    v-if="issues.length > 0"
    ref="summary"
    class="editor-error-summary"
    aria-labelledby="editor-error-title"
    tabindex="-1"
  >
    <h2 id="editor-error-title">
      Check these fields
    </h2>
    <ul>
      <li
        v-for="(issue, index) in issues"
        :key="`${issue.path}:${issue.code}:${index}`"
      >
        <button
          type="button"
          @click="emit('focusIssue', issue.path)"
        >
          {{ safeText(issue.code) }}
        </button>
      </li>
    </ul>
  </section>
</template>
