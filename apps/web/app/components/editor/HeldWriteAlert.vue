<script setup lang="ts">
import { computed } from 'vue';

import { Button } from '@/components/ui/button';

import type { ResumeEditorActions } from '../../composables/useResumeEditor';
import type { ResumeRecord } from '../../stores/resumes';
import StatusBanner from '../app/StatusBanner.vue';
import { editorShellCopy } from '../../i18n/editor-shell';

const props = defineProps<{
  readonly actions: ResumeEditorActions;
  readonly record: ResumeRecord;
}>();
const { locale } = useLocale();
const copy = computed(() => editorShellCopy[locale.value]);

const held = computed(() => {
  const attempt = props.record.attempt;
  if (
    attempt === null
    || props.record.conflicts.some(
      ({ id }) => id === attempt.queueItem.id || id === attempt.command.id,
    )
  ) {
    return undefined;
  }
  if (attempt.kind === 'unknown' && attempt.alert === true) {
    return {
      kind: 'unknown' as const,
      commandId: attempt.command.id,
      sending: attempt.sending === true,
      title: copy.value.unknownWriteTitle,
      description: copy.value.unknownWriteDescription,
      action: copy.value.retryNow,
    };
  }
  if (attempt.kind === 'failed' && attempt.reason === 'stale-limit') {
    return {
      kind: 'stale' as const,
      commandId: attempt.command.id,
      sending: attempt.sending === true,
      title: copy.value.staleWriteTitle,
      description: copy.value.staleWriteDescription,
      action: copy.value.tryAgain,
    };
  }
  return undefined;
});

function retry(commandId: string): void {
  void props.actions.retry(commandId);
}
</script>

<template>
  <StatusBanner
    v-if="held !== undefined"
    :data-held-write="held.kind"
    kind="error"
    :title="held.title"
  >
    <p>{{ held.description }}</p>
    <Button
      data-action="retry-held-write"
      :disabled="held.sending"
      size="sm"
      type="button"
      @click="retry(held.commandId)"
    >
      {{ held.action }}
    </Button>
  </StatusBanner>
</template>
