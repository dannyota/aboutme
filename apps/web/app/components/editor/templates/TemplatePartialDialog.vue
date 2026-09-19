<script setup lang="ts">
import { ref, computed } from 'vue';
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

import LocaleToggle from '@/components/app/LocaleToggle.vue';
import { editorShellCopy } from '@/i18n/editor-shell';
import { editorControlsCopy } from '../../../i18n/editor-controls';

import type { ResumeEditorActions } from '../../../composables/useResumeEditor';
import type {
  TemplateGroupCommand,
  TemplateGroupState,
  TemplateRecovery,
} from '../../../editor/templateGroup';

type RecoveryAction = 'retry-remaining' | 'restore-pre-apply' | 'keep-partial';

const props = defineProps<{
  readonly actions: ResumeEditorActions;
  readonly group: TemplateGroupCommand;
  readonly state: Extract<TemplateGroupState, { kind: 'partial' }>;
}>();

const retryButton = ref<{ $el?: HTMLElement } | null>(null);
type RecoveryReason = Extract<
  TemplateRecovery,
  { kind: 'unavailable' }
>['reason'];
const reason = ref<RecoveryReason | null>(null);
const { locale } = useLocale();
const copy = computed(() => editorControlsCopy[locale.value].controls);

function onOpenAutoFocus(event: Event): void {
  event.preventDefault();
  retryButton.value?.$el?.focus();
}

function recover(action: RecoveryAction): void {
  reason.value = null;
  const result = props.actions.recoverTemplate(action);
  if (result.kind === 'unavailable') reason.value = result.reason;
}

function childStatusLabel(
  kind: TemplateGroupCommand['children'][number]['kind'],
): string {
  return childStatus(kind) === 'accepted'
    ? copy.value.warningAccepted
    : copy.value.warningRemains;
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

const reasonText = computed(() => {
  switch (reason.value) {
    case 'state-changed':
      return copy.value.templateUndoUnavailable;
    case 'context-changed':
      return copy.value.templateCurrentChanged;
    case 'read-required':
      return copy.value.templateReadRequired;
    case null:
      return '';
  }
  return '';
});

function stateMessage(): string {
  switch (props.state.reason) {
    case 'child-failed':
    case 'canonicalized':
    case 'remote-change':
    case 'superseded-after-success':
    case 'context-change':
    case 'unknown-outcome':
      return copy.value.templateResultReview;
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
    <AlertDialogContent @open-auto-focus="onOpenAutoFocus">
      <AlertDialogHeader>
        <AlertDialogTitle>{{ copy.templateChangesReview }}</AlertDialogTitle>
        <AlertDialogDescription>{{ stateMessage() }}</AlertDialogDescription>
        <LocaleToggle
          :label="editorShellCopy[locale].localeLabel"
          @pointerdown.prevent
        />
      </AlertDialogHeader>
      <ul :aria-label="copy.templateChangeProgress">
        <li v-if="childStatus('structure') !== ''">
          {{ copy.warningPlacement }} {{ childStatusLabel('structure') }}.
        </li>
        <li v-if="childStatus('customization') !== ''">
          {{ copy.warningCustomization }}
          {{ childStatusLabel('customization') }}.
        </li>
      </ul>
      <StatusBanner
        v-if="reasonText !== ''"
        kind="error"
      >
        {{ reasonText }}
      </StatusBanner>
      <!-- The footer stacks in reverse on phones, so the primary action
           comes last here to show first there (DESIGN.md). -->
      <AlertDialogFooter>
        <Button
          type="button"
          data-action="keep-partial"
          variant="outline"
          @click="recover('keep-partial')"
        >
          {{ copy.keepPartial }}
        </Button>
        <Button
          type="button"
          data-action="restore-pre-apply"
          variant="outline"
          @click="recover('restore-pre-apply')"
        >
          {{ copy.restorePreApply }}
        </Button>
        <Button
          ref="retryButton"
          type="button"
          data-action="retry-remaining"
          @click="recover('retry-remaining')"
        >
          {{ copy.retryRemaining }}
        </Button>
      </AlertDialogFooter>
    </AlertDialogContent>
  </AlertDialog>
</template>
