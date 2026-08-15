<script setup lang="ts">
import { computed, nextTick, ref } from 'vue';

import type { ResumeEditorActions } from '../../../composables/useResumeEditor';
import type { AtomicConflictRecord } from '../../../editor/conflicts';
import type { ResumeRecord } from '../../../stores/resumes';
import CropEditor from './CropEditor.vue';

const props = defineProps<{
  readonly actions: ResumeEditorActions;
  readonly record: ResumeRecord;
}>();

const pendingDeleteBinding = ref<string | null>(null);
const confirmDeleteButton = ref<HTMLButtonElement | null>(null);
const deleteOpener = ref<HTMLElement | null>(null);
const deleteStatus = ref('');
const opaqueReplacement = ref<File | null>(null);
const photo = computed(
  () => props.record.current.document.personalDetails.photo,
);
const read = computed(() => props.record.photoRead);
const opaque = computed(() => props.record.opaquePhotoOutcome);
const cropConflict = computed(() =>
  props.record.conflicts.find((value) => isChangedCropConflict(value)),
);
const retryCommandId = computed(() => {
  const attempt = props.record.attempt;
  if (
    attempt?.kind !== 'retry-later'
    || !isPhotoCommand(attempt.command.kind)
  ) {
    return undefined;
  }
  return attempt.command.id;
});

function upload(event: Event): void {
  const file = fileFrom(event);
  if (file === undefined) return;
  if (opaque.value !== null) {
    opaqueReplacement.value = file;
    return;
  }
  props.actions.edit({ kind: 'photoUpload', file });
}

function requestDelete(event: MouseEvent): void {
  const binding = photo.value?.key;
  if (binding === undefined) return;
  deleteOpener.value = event.currentTarget instanceof HTMLElement
    ? event.currentTarget
    : null;
  deleteStatus.value = '';
  pendingDeleteBinding.value = binding;
  void nextTick(() => confirmDeleteButton.value?.focus());
}

function confirmDelete(): void {
  if (pendingDeleteBinding.value !== photo.value?.key) {
    deleteStatus.value = 'This photo changed. Reopen deletion and '
      + 'confirm again.';
    closeDeleteDialog();
    return;
  }
  closeDeleteDialog();
  props.actions.edit({ kind: 'photoDelete' });
}

function cancelDelete(): void {
  closeDeleteDialog();
}

function closeDeleteDialog(): void {
  pendingDeleteBinding.value = null;
  void nextTick(() => deleteOpener.value?.focus());
}

function trapDeleteFocus(event: KeyboardEvent): void {
  if (event.key === 'Escape') {
    event.preventDefault();
    cancelDelete();
    return;
  }
  if (event.key !== 'Tab') return;
  const dialog = event.currentTarget;
  if (!(dialog instanceof HTMLElement)) return;
  const buttons = [...dialog.querySelectorAll<HTMLButtonElement>('button')];
  const first = buttons[0];
  const last = buttons.at(-1);
  if (first === undefined || last === undefined) return;
  if (event.shiftKey && document.activeElement === first) {
    event.preventDefault();
    last.focus();
  } else if (!event.shiftKey && document.activeElement === last) {
    event.preventDefault();
    first.focus();
  }
}

function keepObserved(): void {
  const command = opaque.value?.command;
  if (command === undefined) return;
  void props.actions.resolveOpaquePhoto(command.id, {
    kind: 'keep-observed',
  });
}

function replaceObserved(): void {
  const command = opaque.value?.command;
  const file = opaqueReplacement.value;
  if (command === undefined || file === null) return;
  opaqueReplacement.value = null;
  void props.actions.resolveOpaquePhoto(command.id, { kind: 'replace', file });
}

function reopenCrop(): void {
  const conflict = cropConflict.value;
  if (conflict === undefined) return;
  void props.actions.acceptLatest(conflict.id);
}

function retryPhoto(): void {
  if (retryCommandId.value === undefined) return;
  void props.actions.retry(retryCommandId.value);
}

function fileFrom(event: Event): File | undefined {
  const target = event.target;
  if (!(target instanceof HTMLInputElement)) return undefined;
  return target.files?.item(0) ?? undefined;
}

function previewText(): string {
  switch (read.value.kind) {
    case 'ready':
      return 'Authorized photo preview.';
    case 'loading':
      return 'Photo preview is loading.';
    case 'suspended':
      return 'Photo preview is unavailable.';
    case 'none':
      return photo.value === undefined
        ? 'No photo has been added.'
        : 'Photo preview is unavailable.';
  }
}

function statusText(): string | undefined {
  const attempt = props.record.attempt;
  if (props.record.sessionLost) {
    return 'Your session ended. Sign in to continue.';
  }
  if (attempt?.kind === 'retry-later') {
    const wait
      = attempt.retryAfterMs === null
        ? 'Please try again later.'
        : `Try again in ${Math.ceil(attempt.retryAfterMs / 1_000)} seconds.`;
    return attempt.reason === 'media-busy'
      ? `Photo processing is busy. ${wait}`
      : `Too many photo requests. ${wait}`;
  }
  if (
    attempt?.kind === 'dispatching'
    && attempt.command.kind === 'photoUpload'
  ) {
    return 'Uploading photo.';
  }
  if (attempt?.kind === 'unknown') {
    return 'We could not confirm the photo request.';
  }
  if (attempt?.kind !== 'failed') return undefined;
  switch (attempt.reason) {
    case 'media_type_unsupported':
      return 'Choose a JPEG or PNG image.';
    case 'media_too_large':
      return 'This image exceeds the allowed size.';
    case 'media_invalid':
      return 'This image could not be used.';
    case 'precondition_required':
    case 'precondition_malformed':
      return 'The photo changed. Refresh and try again.';
    default:
      return 'The photo request needs attention.';
  }
}

function observedText(): string {
  switch (opaque.value?.observed) {
    case 'unchanged':
      return 'The observed photo is unchanged.';
    case 'changed':
      return 'The observed photo changed.';
    default:
      return 'The observed photo is unavailable.';
  }
}

function isChangedCropConflict(value: unknown): value is AtomicConflictRecord {
  return (
    typeof value === 'object'
    && value !== null
    && 'id' in value
    && 'subject' in value
    && 'kind' in value
    && 'command' in value
    && (value as AtomicConflictRecord).subject === 'atomic'
    && (value as AtomicConflictRecord).command.kind === 'photoCrop'
    && (value as AtomicConflictRecord).kind === 'photo-changed'
  );
}

function isPhotoCommand(kind: string): boolean {
  return kind === 'photoUpload'
    || kind === 'photoDelete'
    || kind === 'photoCrop';
}
</script>

<template>
  <section aria-labelledby="photo-title">
    <h2 id="photo-title">
      Photo
    </h2>
    <div
      aria-live="polite"
      data-photo-preview
    >
      <img
        v-if="read.kind === 'ready' && read.binding === photo?.key"
        :src="read.dataUrl"
        alt=""
      >
      <p>{{ previewText() }}</p>
    </div>

    <p
      v-if="statusText() !== undefined"
      role="status"
    >
      {{ statusText() }}
    </p>
    <p
      v-if="deleteStatus !== ''"
      role="status"
    >
      {{ deleteStatus }}
    </p>
    <button
      v-if="retryCommandId !== undefined && opaque === null"
      data-action="retry-photo"
      type="button"
      @click="retryPhoto"
    >
      Retry photo request
    </button>
    <div
      v-if="opaque !== null"
      data-photo-outcome
    >
      <p>We could not confirm whether the upload changed your photo.</p>
      <p>{{ observedText() }}</p>
      <label>
        Select a replacement photo
        <input
          accept="image/jpeg,image/png"
          type="file"
          @change="upload"
        >
      </label>
      <button
        data-action="keep-observed"
        type="button"
        @click="keepObserved"
      >
        Keep observed photo
      </button>
      <button
        data-action="replace"
        type="button"
        :disabled="opaqueReplacement === null"
        @click="replaceObserved"
      >
        Replace photo
      </button>
    </div>
    <template v-if="opaque === null">
      <label>
        {{ photo === undefined ? "Upload photo" : "Replace photo" }}
        <input
          accept="image/jpeg,image/png"
          type="file"
          @change="upload"
        >
      </label>

      <template v-if="photo !== undefined">
        <button
          data-action="delete"
          type="button"
          @click="requestDelete"
        >
          Delete photo
        </button>
        <div
          v-if="pendingDeleteBinding !== null"
          aria-describedby="photo-delete-description"
          aria-labelledby="photo-delete-title"
          aria-modal="true"
          role="alertdialog"
          @keydown="trapDeleteFocus"
        >
          <h3 id="photo-delete-title">
            Delete photo
          </h3>
          <p id="photo-delete-description">
            Delete the current photo?
          </p>
          <button
            ref="confirmDeleteButton"
            data-action="confirm-delete"
            type="button"
            @click="confirmDelete"
          >
            Delete photo
          </button>
          <button
            data-action="cancel-delete"
            type="button"
            @click="cancelDelete"
          >
            Cancel
          </button>
        </div>
        <CropEditor
          v-if="read.kind === 'ready' && read.binding === photo.key"
          :actions="actions"
          :crop="photo.crop"
          :photo-key="photo.key"
          :photo-url="read.dataUrl"
        />
      </template>
      <div
        v-if="cropConflict !== undefined"
        role="alert"
      >
        <p>The photo changed. Reopen crop against the current photo.</p>
        <button
          data-action="reopen-crop"
          type="button"
          @click="reopenCrop"
        >
          Reopen crop
        </button>
      </div>
    </template>
  </section>
</template>
