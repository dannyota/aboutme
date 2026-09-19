<script setup lang="ts">
import { computed, nextTick, ref, watch } from 'vue';

import { Button } from '@/components/ui/button';
import StatusBanner from '../app/StatusBanner.vue';
import type { ServerValidationIssue } from '../../editor/attempt';
import { editorShellCopy } from '../../i18n/editor-shell';

const props = defineProps<{
  readonly issues: readonly ServerValidationIssue[];
}>();
const emit = defineEmits<{
  focusIssue: [path: string];
}>();
const summary = ref<{ focus?: () => void } | null>(null);
const { locale } = useLocale();
const copy = computed(() => editorShellCopy[locale.value]);

watch(
  () => props.issues.map(({ path, code }) => `${path}:${code}`).join('|'),
  async (next, previous) => {
    if (next === '' || next === previous) return;
    await nextTick();
    summary.value?.focus?.();
  },
);
</script>

<template>
  <StatusBanner
    v-if="issues.length > 0"
    ref="summary"
    class="editor-error-summary"
    :focus-on-mount="false"
    kind="error"
    :title="copy.checkFields"
  >
    <ul>
      <li
        v-for="(issue, index) in issues"
        :key="`${issue.path}:${issue.code}:${index}`"
      >
        <Button
          size="sm"
          type="button"
          variant="link"
          data-action="focus-editor-issue"
          @click="emit('focusIssue', issue.path)"
        >
          {{ copy.issueFor(issue.code) }}
        </Button>
      </li>
    </ul>
  </StatusBanner>
</template>
