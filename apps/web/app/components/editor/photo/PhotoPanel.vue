<script setup lang="ts">
import { computed, ref, useId } from 'vue';
import { ImageOff, Upload } from '@lucide/vue';
import ConfirmDialog from '@/components/app/ConfirmDialog.vue';
import InspectorPanel from '@/components/editor/InspectorPanel.vue';
import StatusBanner from '@/components/app/StatusBanner.vue';
import { Button } from '@/components/ui/button';
import { Card } from '@/components/ui/card';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';

import type { ResumeEditorActions } from '../../../composables/useResumeEditor';
import type { AtomicConflictRecord } from '../../../editor/conflicts';
import type { ResumeRecord } from '../../../stores/resumes';
import CropEditor from './CropEditor.vue';

const props = defineProps<{
  readonly actions: ResumeEditorActions;
  readonly record: ResumeRecord;
}>();

const pendingDeleteBinding = ref<string | null>(null);
const deleteStatus = ref('');
const opaqueReplacement = ref<File | null>(null);
const uploadId = `photo-upload-${useId()}`;
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

function requestDelete(): void {
  const binding = photo.value?.key;
  if (binding === undefined) return;
  deleteStatus.value = '';
  pendingDeleteBinding.value = binding;
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

function statusKind(): 'info' | 'error' {
  return props.record.attempt?.kind === 'failed' ? 'error' : 'info';
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
  <InspectorPanel
    title="Photo"
    title-id="photo-title"
  >
    <Card
      aria-live="polite"
      data-photo-preview
      class="flex flex-col items-center gap-2"
    >
      <div class="size-32 rounded-lg border bg-muted">
        <img
          v-if="read.kind === 'ready' && read.binding === photo?.key"
          data-photo-image
          class="size-full rounded-lg object-cover"
          :src="read.dataUrl"
          alt=""
        >
        <ImageOff
          v-else
          aria-hidden="true"
          class="size-8 text-muted-foreground"
        />
      </div>
      <p class="text-sm text-muted-foreground">
        {{ previewText() }}
      </p>
    </Card>

    <StatusBanner
      v-if="statusText() !== undefined"
      :kind="statusKind()"
    >
      {{ statusText() }}
    </StatusBanner>
    <StatusBanner
      v-if="deleteStatus !== ''"
      kind="error"
    >
      {{ deleteStatus }}
    </StatusBanner>
    <Button
      v-if="retryCommandId !== undefined && opaque === null"
      data-action="retry-photo"
      size="sm"
      type="button"
      @click="retryPhoto"
    >
      Retry photo request
    </Button>
    <Card
      v-if="opaque !== null"
      data-photo-outcome
    >
      <p>We could not confirm whether the upload changed your photo.</p>
      <p>{{ observedText() }}</p>
      <Label
        class="flex cursor-pointer flex-col items-center gap-1 rounded-lg
          border border-dashed p-4 text-sm hover:bg-accent"
        :for="uploadId"
      >
        <Upload
          aria-hidden="true"
          class="size-5 text-muted-foreground"
        />
        <span>Select a replacement photo</span>
        <Input
          :id="uploadId"
          accept="image/jpeg,image/png"
          class="sr-only"
          data-action="upload-photo-input"
          type="file"
          @change="upload"
        />
      </Label>
      <Button
        data-action="keep-observed"
        size="sm"
        type="button"
        variant="ghost"
        @click="keepObserved"
      >
        Keep observed photo
      </Button>
      <Button
        data-action="replace"
        size="sm"
        type="button"
        variant="ghost"
        :disabled="opaqueReplacement === null"
        @click="replaceObserved"
      >
        Replace photo
      </Button>
    </Card>
    <template v-if="opaque === null">
      <Label
        class="flex cursor-pointer flex-col items-center gap-1 rounded-lg
          border border-dashed p-4 text-sm hover:bg-accent"
        :for="uploadId"
      >
        <Upload
          aria-hidden="true"
          class="size-5 text-muted-foreground"
        />
        <span class="font-medium">
          {{ photo === undefined ? 'Upload photo' : 'Replace photo' }}
        </span>
        <span class="text-xs text-muted-foreground">
          JPEG or PNG, up to 2 MB.
        </span>
        <Input
          :id="uploadId"
          accept="image/jpeg,image/png"
          class="sr-only"
          data-action="upload-photo-input"
          type="file"
          @change="upload"
        />
      </Label>

      <template v-if="photo !== undefined">
        <Button
          data-action="delete"
          size="sm"
          type="button"
          variant="ghost"
          @click="requestDelete"
        >
          Delete photo
        </Button>
        <ConfirmDialog
          :open="pendingDeleteBinding !== null"
          title="Delete photo"
          description="Delete the current photo?"
          confirm-label="Delete photo"
          destructive
          confirm-action="confirm-delete"
          cancel-action="cancel-delete"
          @confirm="confirmDelete"
          @cancel="cancelDelete"
        />
        <Card v-if="read.kind === 'ready' && read.binding === photo.key">
          <CropEditor
            :actions="actions"
            :crop="photo.crop"
            :photo-key="photo.key"
            :photo-url="read.dataUrl"
          />
        </Card>
      </template>
      <StatusBanner
        v-if="cropConflict !== undefined"
        kind="info"
      >
        <p>The photo changed. Reopen crop against the current photo.</p>
        <Button
          data-action="reopen-crop"
          type="button"
          @click="reopenCrop"
        >
          Reopen crop
        </Button>
      </StatusBanner>
    </template>
  </InspectorPanel>
</template>
