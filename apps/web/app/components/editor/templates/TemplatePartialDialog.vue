<script setup lang="ts">
import { nextTick, onMounted, ref } from 'vue';
import { Button } from '@/components/ui/button';
import {
  AlertDialog,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog';
import StatusBanner from '@/components/app/StatusBanner.vue';

import type { ResumeEditorActions } from '../../../composables/useResumeEditor';
import type {
  TemplateGroupCommand,
  TemplateGroupState,
} from '../../../editor/templateGroup';

type RecoveryAction = 'retry-remaining' | 'restore-pre-apply' | 'keep-partial';

const props = defineProps<{
  readonly actions: ResumeEditorActions;
  readonly group: TemplateGroupCommand;
  readonly state: Extract<TemplateGroupState, { kind: 'partial' }>;
}>();

const retryButton = ref<{ $el?: HTMLElement } | null>(null);
const reason = ref('');

onMounted(() => {
  void nextTick(() => retryButton.value?.$el?.focus());
});

function recover(action: RecoveryAction): void {
  reason.value = '';
  const result = props.actions.recoverTemplate(action);
  if (result.kind === 'unavailable') reason.value = messageFor(result.reason);
}

function childStatus(
  kind: TemplateGroupCommand['children'][number]['kind'],
): string {
  const child = props.group.children.findIndex(
    (candidate) => candidate.kind === kind,
  );
  if (child < 0) return '';
  return child < props.state.nextChild ? 'accepted' : 'remains';
}

function messageFor(
  unavailable: 'state-changed' | 'context-changed' | 'read-required',
): string {
  switch (unavailable) {
    case 'state-changed':
      return 'The template changes no longer match the current resume.';
    case 'context-changed':
      return [
        'The resume context changed.',
        'Review the current resume before trying again.',
      ].join(' ');
    case 'read-required':
      return 'Refresh the complete resume before trying again.';
  }
}

function stateMessage(): string {
  switch (props.state.reason) {
    case 'child-failed':
    case 'canonicalized':
    case 'remote-change':
    case 'superseded-after-success':
    case 'context-change':
    case 'unknown-outcome':
      return 'The template result needs review.';
    default:
      return assertNever(props.state.reason);
  }
}

function assertNever(value: never): never {
  throw new Error(`Unexpected template partial state: ${String(value)}`);
}
</script>

<template>
  <AlertDialog :open="true">
    <AlertDialogContent>
      <AlertDialogHeader>
        <AlertDialogTitle>Template changes need review</AlertDialogTitle>
        <AlertDialogDescription>{{ stateMessage() }}</AlertDialogDescription>
      </AlertDialogHeader>
      <ul aria-label="Template change progress">
        <li v-if="childStatus('structure') !== ''">
          Placement change {{ childStatus('structure') }}.
        </li>
        <li v-if="childStatus('customization') !== ''">
          Customization change {{ childStatus('customization') }}.
        </li>
      </ul>
      <StatusBanner
        v-if="reason !== ''"
        kind="error"
      >
        {{ reason }}
      </StatusBanner>
      <AlertDialogFooter>
        <Button
          ref="retryButton"
          type="button"
          data-action="retry-remaining"
          @click="recover('retry-remaining')"
        >
          Retry remaining
        </Button>
        <Button
          type="button"
          data-action="restore-pre-apply"
          @click="recover('restore-pre-apply')"
        >
          Restore pre-apply
        </Button>
        <Button
          type="button"
          data-action="keep-partial"
          @click="recover('keep-partial')"
        >
          Keep partial
        </Button>
      </AlertDialogFooter>
    </AlertDialogContent>
  </AlertDialog>
</template>
