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
import {
  templateChildApplied,
  templateRecoveryAvailable,
} from '../../../editor/templateGroup';
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
const keepButton = ref<{ $el?: HTMLElement } | null>(null);
type RecoveryReason = Extract<
  TemplateRecovery,
  { kind: 'unavailable' }
>['reason'];
const reason = ref<RecoveryReason | null>(null);
const { locale } = useLocale();
const copy = computed(() => editorControlsCopy[locale.value].controls);

// Recovery reads the latest accepted resume, as recoverTemplate does.
const latest = computed(
  () => props.actions.record.value?.accepted ?? props.state.accepted,
);
const canRetry = computed(() =>
  templateRecoveryAvailable(
    props.group,
    props.state,
    latest.value,
    'retry-remaining',
  ),
);
const canRestore = computed(() =>
  templateRecoveryAvailable(
    props.group,
    props.state,
    latest.value,
    'restore-pre-apply',
  ),
);
const parts = computed(() =>
  props.group.children.map((child) => ({
    kind: child.kind,
    name: child.kind === 'structure'
      ? copy.value.templatePartLayout
      : copy.value.templatePartStyle,
    status: templateChildApplied(child, latest.value)
      ? copy.value.templatePartApplied
      : copy.value.templatePartNotApplied,
  })),
);
const missing = computed(() =>
  props.group.children.some(
    (child) => !templateChildApplied(child, latest.value),
  ),
);

function onOpenAutoFocus(event: Event): void {
  event.preventDefault();
  (retryButton.value ?? keepButton.value)?.$el?.focus();
}

function recover(action: RecoveryAction): void {
  reason.value = null;
  const result = props.actions.recoverTemplate(action);
  if (result.kind === 'unavailable') reason.value = result.reason;
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
      return copy.value.templatePartialFailed;
    case 'unknown-outcome':
      return copy.value.templatePartialConnection;
    case 'canonicalized':
    case 'remote-change':
    case 'superseded-after-success':
    case 'context-change':
      return copy.value.templatePartialChanged;
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
        <AlertDialogTitle>{{ copy.templatePartialTitle }}</AlertDialogTitle>
        <AlertDialogDescription>{{ stateMessage() }}</AlertDialogDescription>
        <LocaleToggle
          :label="editorShellCopy[locale].localeLabel"
          @pointerdown.prevent
        />
      </AlertDialogHeader>
      <ul :aria-label="copy.templateChangeProgress">
        <li
          v-for="part in parts"
          :key="part.kind"
          :data-template-part="part.kind"
        >
          {{ copy.templatePart(part.name, part.status) }}
        </li>
      </ul>
      <ul class="text-sm">
        <li>{{ copy.templateKeepHint }}</li>
        <li v-if="canRestore">
          {{ copy.templateUndoHint }}
        </li>
        <li v-if="canRetry">
          {{ copy.templateRetryHint }}
        </li>
      </ul>
      <p
        v-if="missing && !canRetry"
        class="text-sm"
      >
        {{ copy.templateApplyAgain }}
      </p>
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
          ref="keepButton"
          type="button"
          data-action="keep-partial"
          variant="outline"
          @click="recover('keep-partial')"
        >
          {{ copy.keepPartial }}
        </Button>
        <Button
          v-if="canRestore"
          type="button"
          data-action="restore-pre-apply"
          variant="outline"
          @click="recover('restore-pre-apply')"
        >
          {{ copy.restorePreApply }}
        </Button>
        <Button
          v-if="canRetry"
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
