<script setup lang="ts">
import { computed, ref } from 'vue';

import {
  AlertDialog,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog';
import { Button } from '@/components/ui/button';
import type { HistoryController } from '../../../editor/historyController';
import { canonicalPublicPath } from '../../../editor/publishApi';
import { editorHistoryCopy } from '../../../i18n/editor-history';
import { editorShellCopy } from '../../../i18n/editor-shell';
import type { ResumeRecord } from '../../../stores/resumes';
import StatusBanner from '../../app/StatusBanner.vue';
import { formatAbsolute, languageName } from './historyFormat';
import { isUnsetLanguage } from '../resumeLanguage';

const props = defineProps<{
  readonly controller: HistoryController;
  readonly record: ResumeRecord;
}>();

const { locale } = useLocale();
const copy = computed(() => editorHistoryCopy[locale.value]);
const shell = computed(() => editorShellCopy[locale.value]);
const cancelButton = ref<{ $el?: HTMLElement } | null>(null);

const dialog = computed(() => props.controller.restore.value);
const phase = computed(() => dialog.value?.phase ?? 'confirm');
const busy = computed(() => phase.value === 'busy');
const hidesRestore = computed(
  () => phase.value === 'invalid' || phase.value === 'gone',
);
const restoreDisabled = computed(() => busy.value || phase.value === 'later');

const entry = computed(() => {
  const id = dialog.value?.id;
  if (props.controller.list.value.kind !== 'ready') return undefined;
  return props.controller.list.value.items.find((item) => item.id === id);
});
const startedAt = computed(
  () => entry.value?.startedAt ?? dialog.value?.version?.startedAt ?? '',
);
const time = computed(() =>
  formatAbsolute(startedAt.value, locale.value, new Date()),
);

const link = computed(() => {
  const metadata = props.record.accepted.metadata;
  const path = metadata.live ? canonicalPublicPath(metadata.slug) : null;
  return path === null ? null : `aboutme.vn${path}`;
});
const versionTitle = computed(
  () => dialog.value?.version?.title ?? entry.value?.title ?? null,
);
const newTitle = computed(() =>
  versionTitle.value !== null
  && versionTitle.value !== props.record.accepted.metadata.title
    ? versionTitle.value
    : null,
);
const newLanguage = computed(() => {
  const version = dialog.value?.version;
  if (version === null || version === undefined) return null;
  const current = props.record.accepted.metadata.lng;
  const same = (isUnsetLanguage(version.lng) && isUnsetLanguage(current))
    || version.lng === current;
  return same ? null : languageName(version.lng, locale.value);
});

const alert = computed(() => {
  switch (phase.value) {
    case 'changed':
      return copy.value.restoreChanged;
    case 'invalid':
      return copy.value.restoreInvalid;
    case 'gone':
      return copy.value.versionGone;
    case 'network':
      return copy.value.restoreNetwork;
    case 'later':
    case 'retry':
      return copy.value.restoreBusy;
    case 'failed':
      return copy.value.restoreFailed;
    default:
      return null;
  }
});
const confirmLabel = computed(() => {
  if (busy.value) return copy.value.restoring;
  return phase.value === 'network' ? shell.value.tryAgain : copy.value.restore;
});
const cancelLabel = computed(() =>
  hidesRestore.value ? shell.value.close : copy.value.cancel,
);

function onOpenChange(open: boolean): void {
  if (!open) props.controller.closeRestore();
}
function guardBusy(event: Event): void {
  if (busy.value) event.preventDefault();
}
function onOpenAutoFocus(event: Event): void {
  event.preventDefault();
  cancelButton.value?.$el?.focus();
}
</script>

<template>
  <AlertDialog
    :open="dialog !== null"
    @update:open="onOpenChange"
  >
    <AlertDialogContent
      :aria-busy="busy || undefined"
      class="max-w-[32rem] sm:max-w-[32rem]"
      @escape-key-down="guardBusy"
      @interact-outside="guardBusy"
      @open-auto-focus="onOpenAutoFocus"
    >
      <AlertDialogHeader>
        <AlertDialogTitle>{{ copy.restoreTitle(time) }}</AlertDialogTitle>
        <AlertDialogDescription
          as="div"
          class="grid gap-3 text-sm leading-normal"
        >
          <p class="font-semibold text-foreground">
            <template v-if="link !== null">
              {{ copy.restorePublic(link) }}
            </template>
            <template v-else>
              {{ copy.restorePrivate }}
            </template>
          </p>
          <p>{{ copy.restoreWhat(time) }}</p>
          <p v-if="newTitle !== null">
            {{ copy.restoreNewTitle(newTitle) }}
          </p>
          <p v-if="newLanguage !== null">
            {{ copy.restoreNewLanguage(newLanguage) }}
          </p>
          <p>{{ copy.restoreKept }}</p>
          <p>{{ copy.restoreUndoNote }}</p>
        </AlertDialogDescription>
      </AlertDialogHeader>
      <StatusBanner
        v-if="alert !== null"
        class="mt-0"
        kind="error"
      >
        {{ alert }}
      </StatusBanner>
      <AlertDialogFooter>
        <Button
          ref="cancelButton"
          data-action="restore-cancel"
          :disabled="busy"
          type="button"
          variant="outline"
          @click="controller.closeRestore()"
        >
          {{ cancelLabel }}
        </Button>
        <Button
          v-if="!hidesRestore"
          data-action="restore-confirm"
          :disabled="restoreDisabled"
          type="button"
          @click="controller.confirmRestore()"
        >
          {{ confirmLabel }}
        </Button>
      </AlertDialogFooter>
    </AlertDialogContent>
  </AlertDialog>
</template>
