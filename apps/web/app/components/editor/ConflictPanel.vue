<script setup lang="ts">
import { computed } from 'vue';

import { Button } from '@/components/ui/button';

import type { ResumeEditorActions } from '../../composables/useResumeEditor';
import type { ConflictRecord } from '../../editor/reconcile';
import StatusBanner from '../app/StatusBanner.vue';
import {
  editorShellCopy,
  type ConflictControlKind,
} from '../../i18n/editor-shell';

const props = defineProps<{
  readonly actions: ResumeEditorActions;
  readonly conflicts: readonly ConflictRecord[];
}>();
const emit = defineEmits<{
  openInspector: [target: InspectorTarget];
}>();
const { locale } = useLocale();
const copy = computed(() => editorShellCopy[locale.value]);

type InspectorTarget
  = | { readonly kind: 'section'; readonly key: string }
    | { readonly kind: 'structure' | 'templates' | 'photo' };
type ConflictControl = {
  readonly [Kind in ConflictControlKind]: { readonly kind: Kind };
}[ConflictControlKind];

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
    return { kind: 'review-template' };
  }
  switch (conflict.command.kind) {
    case 'metadataField':
    case 'personalField':
    case 'sectionMetadata':
    case 'customization':
      return canApplyField(conflict)
        ? { kind: 'apply-field' }
        : { kind: 'reopen-placement' };
    case 'entryField':
      return canApplyField(conflict)
        ? { kind: 'apply-field' }
        : { kind: 'select-entry' };
    case 'entryUpsert':
      return { kind: 'recreate-entry' };
    case 'entryDelete':
    case 'resumeDelete':
      return { kind: 'confirm-deletion' };
    case 'entryReorder':
      return { kind: 'reopen-entry-order' };
    case 'structure':
      return { kind: 'reopen-placement' };
    case 'photoCrop':
      return { kind: 'reopen-crop' };
    case 'photoDelete':
    case 'photoUpload':
      return { kind: 'review-photo' };
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

function controlLabel(conflict: ConflictRecord): string {
  const control = controlFor(conflict);
  return control === undefined ? '' : copy.value.conflictControl(control.kind);
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
    :title="copy.conflictTitle"
  >
    <article
      v-for="conflict in conflicts"
      :key="conflict.id"
      :data-conflict="conflictKey(conflict)"
    >
      <p>
        {{ copy.conflictDescription }}
      </p>
      <Button
        v-if="canAcceptLatest(conflict)"
        size="sm"
        type="button"
        @click="acceptLatest(conflict.id)"
      >
        {{ copy.acceptLatest }}
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
        {{ controlLabel(conflict) }}
      </Button>
    </article>
  </StatusBanner>
</template>
