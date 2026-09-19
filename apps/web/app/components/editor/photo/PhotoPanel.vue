<script setup lang="ts">
import { computed, ref, useId, watch } from 'vue';
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
import LocaleToggle from '@/components/app/LocaleToggle.vue';
import { editorShellCopy } from '@/i18n/editor-shell';
import { editorControlsCopy } from '../../../i18n/editor-controls';

const props = defineProps<{
  readonly actions: ResumeEditorActions;
  readonly record: ResumeRecord;
}>();
const { locale } = useLocale();
const copy = computed(() => editorControlsCopy[locale.value].controls);

const pendingDeleteBinding = ref<string | null>(null);
const deleteStatus = ref<'photoChanged' | null>(null);
const deleteStatusText = computed(() =>
  deleteStatus.value === null ? '' : copy.value[deleteStatus.value],
);
const opaqueReplacement = ref<File | null>(null);
const uploadId = `photo-upload-${useId()}`;
const photo = computed(
  () => props.record.current.document.personalDetails.photo,
);
const read = computed(() => props.record.photoRead);
const opaque = computed(() => props.record.opaquePhotoOutcome);
// A photo uploaded here starts on the default crop; one that was already on
// the resume keeps what it has until the person saves a crop.
const awaitingUpload = ref(false);
const uploadedKey = ref<string | null>(null);
watch(
  () => photo.value?.key,
  (key, previous) => {
    if (awaitingUpload.value && key !== undefined && key !== previous) {
      uploadedKey.value = key;
      awaitingUpload.value = false;
    }
  },
);
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
  const result = props.actions.edit({ kind: 'photoUpload', file });
  if (result.kind === 'enqueued') awaitingUpload.value = true;
}

function requestDelete(): void {
  const binding = photo.value?.key;
  if (binding === undefined) return;
  deleteStatus.value = null;
  pendingDeleteBinding.value = binding;
}

function confirmDelete(): void {
  if (pendingDeleteBinding.value !== photo.value?.key) {
    deleteStatus.value = 'photoChanged';
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
      return copy.value.photoPreview;
    case 'loading':
      return copy.value.photoPreviewLoading;
    case 'suspended':
      return copy.value.photoPreviewUnavailable;
    case 'none':
      return photo.value === undefined
        ? copy.value.noPhoto
        : copy.value.photoPreviewUnavailable;
  }
}

function statusText(): string | undefined {
  const attempt = props.record.attempt;
  if (props.record.sessionLost) {
    return copy.value.photoSessionEnded;
  }
  if (attempt?.kind === 'retry-later') {
    const wait
      = attempt.retryAfterMs === null
        ? copy.value.tryAgainLater()
        : copy.value.tryAgainLater(Math.ceil(attempt.retryAfterMs / 1_000));
    return attempt.reason === 'media-busy'
      ? copy.value.photoStatusBusy(wait)
      : copy.value.photoStatusRateLimited(wait);
  }
  if (
    attempt?.kind === 'dispatching'
    && attempt.command.kind === 'photoUpload'
  ) {
    return copy.value.photoUploading;
  }
  if (attempt?.kind === 'unknown') {
    return copy.value.photoRequestUnknown;
  }
  if (attempt?.kind !== 'failed') return undefined;
  switch (attempt.reason) {
    case 'media_type_unsupported':
      return copy.value.imageType;
    case 'media_too_large':
      return copy.value.imageLarge;
    case 'media_invalid':
      return copy.value.imageInvalid;
    case 'precondition_required':
    case 'precondition_malformed':
      return copy.value.photoPrecondition;
    default:
      return copy.value.photoRequestAttention;
  }
}

function statusKind(): 'info' | 'error' {
  return props.record.attempt?.kind === 'failed' ? 'error' : 'info';
}

function observedText(): string {
  switch (opaque.value?.observed) {
    case 'unchanged':
      return copy.value.observedUnchanged;
    case 'changed':
      return copy.value.observedChanged;
    default:
      return copy.value.observedUnavailable;
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
  return (
    kind === 'photoUpload' || kind === 'photoDelete' || kind === 'photoCrop'
  );
}
</script>

<template>
  <InspectorPanel
    :title="copy.photo"
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
      v-if="deleteStatusText !== ''"
      kind="error"
    >
      {{ deleteStatusText }}
    </StatusBanner>
    <Button
      v-if="retryCommandId !== undefined && opaque === null"
      data-action="retry-photo"
      size="sm"
      type="button"
      @click="retryPhoto"
    >
      {{ copy.photoRetry }}
    </Button>
    <Card
      v-if="opaque !== null"
      data-photo-outcome
    >
      <p>{{ copy.opaquePhoto }}</p>
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
        <span>{{ copy.selectPhoto }}</span>
        <Input
          :id="uploadId"
          accept="image/jpeg,image/png"
          class="sr-only size-px p-0"
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
        {{ copy.keepObservedPhoto }}
      </Button>
      <Button
        data-action="replace"
        size="sm"
        type="button"
        variant="ghost"
        :disabled="opaqueReplacement === null"
        @click="replaceObserved"
      >
        {{ copy.replaceObservedPhoto }}
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
          {{ photo === undefined ? copy.uploadPhoto : copy.replacePhoto }}
        </span>
        <span class="text-xs text-muted-foreground">
          {{ copy.photoUploadHint }}
        </span>
        <Input
          :id="uploadId"
          accept="image/jpeg,image/png"
          class="sr-only size-px p-0"
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
          {{ copy.deletePhoto }}
        </Button>
        <ConfirmDialog
          :open="pendingDeleteBinding !== null"
          :title="copy.deletePhotoTitle"
          :description="copy.deletePhotoDescription"
          :confirm-label="copy.deletePhoto"
          :cancel-label="copy.cancel"
          destructive
          confirm-action="confirm-delete"
          cancel-action="cancel-delete"
          @confirm="confirmDelete"
          @cancel="cancelDelete"
        >
          <template #header-actions>
            <LocaleToggle
              :label="editorShellCopy[locale].localeLabel"
              @pointerdown.prevent
            />
          </template>
        </ConfirmDialog>
        <Card v-if="read.kind === 'ready' && read.binding === photo.key">
          <CropEditor
            :actions="actions"
            :crop="photo.crop"
            :photo-key="photo.key"
            :photo-url="read.dataUrl"
            :save-default="photo.key === uploadedKey"
          />
        </Card>
      </template>
      <StatusBanner
        v-if="cropConflict !== undefined"
        kind="info"
      >
        <p>{{ copy.photoChangedCrop }}</p>
        <Button
          data-action="reopen-crop"
          type="button"
          @click="reopenCrop"
        >
          {{ copy.reopenCrop }}
        </Button>
      </StatusBanner>
    </template>
  </InspectorPanel>
</template>
