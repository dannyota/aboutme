<script setup lang="ts">
import { Button } from '@/components/ui/button';

import type { ResumeEditorActions } from '../../composables/useResumeEditor';
import type { ConflictRecord } from '../../editor/reconcile';
import StatusBanner from '../app/StatusBanner.vue';

const props = defineProps<{
  readonly actions: ResumeEditorActions;
  readonly conflicts: readonly ConflictRecord[];
}>();
const emit = defineEmits<{
  openInspector: [target: InspectorTarget];
}>();

type InspectorTarget
  = | { readonly kind: 'section'; readonly key: string }
    | { readonly kind: 'structure' | 'templates' | 'photo' };
type ConflictControl
  = | { readonly kind: 'apply-field'; readonly label: 'Apply my value' }
    | { readonly kind: 'select-entry'; readonly label: 'Select another entry' }
    | { readonly kind: 'recreate-entry'; readonly label: 'Recreate entry' }
    | {
      readonly kind: 'reopen-entry-order';
      readonly label: 'Reopen entry order';
    }
    | { readonly kind: 'reopen-placement'; readonly label: 'Reopen placement' }
    | { readonly kind: 'reopen-crop'; readonly label: 'Reopen crop' }
    | {
      readonly kind: 'review-template';
      readonly label: 'Review template changes';
    }
    | {
      readonly kind: 'confirm-deletion';
      readonly label: 'Confirm deletion again';
    }
    | { readonly kind: 'review-photo'; readonly label: 'Review photo changes' };

function acceptLatest(id: string): void {
  void props.actions.acceptLatest(id);
}

function canAcceptLatest(conflict: ConflictRecord): boolean {
  return conflict.subject === 'atomic';
}

function canApplyField(conflict: ConflictRecord): boolean {
  if (conflict.subject !== 'atomic') return false;
  return (
    conflict.kind === 'target-changed'
    && (conflict.command.kind === 'metadataField'
      || conflict.command.kind === 'personalField'
      || conflict.command.kind === 'sectionMetadata'
      || conflict.command.kind === 'entryField'
      || conflict.command.kind === 'customization')
  );
}

function applyField(conflict: ConflictRecord): void {
  if (!canApplyField(conflict)) return;
  void props.actions.applyMine(conflict.id, { kind: 'field' });
}

function controlFor(conflict: ConflictRecord): ConflictControl | undefined {
  if (conflict.subject === 'template') {
    return { kind: 'review-template', label: 'Review template changes' };
  }
  switch (conflict.command.kind) {
    case 'metadataField':
    case 'personalField':
    case 'sectionMetadata':
    case 'customization':
      return canApplyField(conflict)
        ? { kind: 'apply-field', label: 'Apply my value' }
        : { kind: 'reopen-placement', label: 'Reopen placement' };
    case 'entryField':
      return canApplyField(conflict)
        ? { kind: 'apply-field', label: 'Apply my value' }
        : { kind: 'select-entry', label: 'Select another entry' };
    case 'entryUpsert':
      return { kind: 'recreate-entry', label: 'Recreate entry' };
    case 'entryDelete':
    case 'resumeDelete':
      return { kind: 'confirm-deletion', label: 'Confirm deletion again' };
    case 'entryReorder':
      return { kind: 'reopen-entry-order', label: 'Reopen entry order' };
    case 'structure':
      return { kind: 'reopen-placement', label: 'Reopen placement' };
    case 'photoCrop':
      return { kind: 'reopen-crop', label: 'Reopen crop' };
    case 'photoDelete':
    case 'photoUpload':
      return { kind: 'review-photo', label: 'Review photo changes' };
    default:
      return assertNever(conflict.command);
  }
}

function useControl(conflict: ConflictRecord): void {
  const control = controlFor(conflict);
  if (control === undefined) return;
  switch (control.kind) {
    case 'apply-field':
      applyField(conflict);
      return;
    case 'recreate-entry':
      if (conflict.subject !== 'atomic') return;
      void props.actions.applyMine(conflict.id, {
        kind: 'recreate',
        newId: props.actions.createEntityId(),
      });
      return;
    case 'reopen-entry-order': {
      if (
        conflict.subject !== 'atomic'
        || conflict.command.kind !== 'entryReorder'
      ) { return; }
      const section
        = conflict.latest.document.content[conflict.command.sectionKey];
      if (section === undefined) {
        reopen(conflict.id, {
          kind: 'section',
          key: conflict.command.sectionKey,
        });
        return;
      }
      void props.actions.applyMine(conflict.id, {
        kind: 'reorder',
        members: section.entries.map((entry) => entry.id),
      });
      emit('openInspector', {
        kind: 'section',
        key: conflict.command.sectionKey,
      });
      return;
    }
    case 'select-entry':
      if (
        conflict.subject !== 'atomic'
        || conflict.command.kind !== 'entryField'
      ) return;
      reopen(conflict.id, {
        kind: 'section',
        key: conflict.command.sectionKey,
      });
      return;
    case 'reopen-placement':
      reopen(conflict.id, { kind: 'structure' });
      return;
    case 'reopen-crop':
    case 'review-photo':
      reopen(conflict.id, { kind: 'photo' });
      return;
    case 'review-template':
      emit('openInspector', { kind: 'templates' });
      return;
    case 'confirm-deletion':
      confirmDestructive(conflict);
      return;
    default:
      return assertNever(control);
  }
}

function reopen(id: string, target: InspectorTarget): void {
  void props.actions.acceptLatest(id);
  emit('openInspector', target);
}

function confirmDestructive(conflict: ConflictRecord): void {
  if (conflict.subject !== 'atomic') return;
  void props.actions.applyMine(conflict.id, {
    kind: 'destructive',
    latestTitle: conflict.latest.metadata.title,
  });
}

function conflictKey(conflict: ConflictRecord): string {
  return conflict.subject === 'template'
    ? 'template'
    : `${conflict.kind}:${conflict.command.kind}`;
}

function assertNever(value: never): never {
  throw new Error(`Unexpected conflict control: ${String(value)}`);
}
</script>

<template>
  <StatusBanner
    v-if="conflicts.length > 0"
    class="editor-conflicts"
    kind="info"
    title="Review changes"
  >
    <article
      v-for="conflict in conflicts"
      :key="conflict.id"
      :data-conflict="conflictKey(conflict)"
    >
      <p>
        This part changed elsewhere. Review the latest version before saving.
      </p>
      <Button
        v-if="canAcceptLatest(conflict)"
        size="sm"
        type="button"
        @click="acceptLatest(conflict.id)"
      >
        Accept latest
      </Button>
      <Button
        v-if="controlFor(conflict) !== undefined"
        :data-action="
          controlFor(conflict)?.kind === 'apply-field'
            ? 'apply-mine'
            : controlFor(conflict)?.kind
        "
        size="sm"
        type="button"
        @click="useControl(conflict)"
      >
        {{ controlFor(conflict)?.label }}
      </Button>
    </article>
  </StatusBanner>
</template>
