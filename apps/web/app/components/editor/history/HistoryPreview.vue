<script setup lang="ts">
import type { Resume } from '@aboutme/schema';
import { Trash2 } from '@lucide/vue';
import { computed, ref } from 'vue';

import { Button } from '@/components/ui/button';
import type { HistoryController } from '../../../editor/historyController';
import { editorHistoryCopy } from '../../../i18n/editor-history';
import { editorShellCopy } from '../../../i18n/editor-shell';
import type { PhotoReadState, ResumeRecord } from '../../../stores/resumes';
import IconButton from '../../app/IconButton.vue';
import LoadingState from '../../app/LoadingState.vue';
import EditorPreview from '../EditorPreview.vue';
import { formatAbsolute } from './historyFormat';

const props = defineProps<{
  readonly controller: HistoryController;
  readonly record: ResumeRecord;
  readonly restoreBlocked: boolean;
  readonly photoUrl?: string;
  readonly photoRead?: PhotoReadState;
}>();
const emit = defineEmits<{ back: []; escape: [] }>();

const { locale } = useLocale();
const copy = computed(() => editorHistoryCopy[locale.value]);
const shell = computed(() => editorShellCopy[locale.value]);
const title = ref<HTMLElement | null>(null);

const state = computed(() => props.controller.preview.value);
const version = computed(() =>
  state.value.kind === 'ready' ? state.value.version : null,
);
const time = computed(() => {
  const id = props.controller.selected.value;
  const list = props.controller.list.value;
  const entry = list.kind === 'ready'
    ? list.items.find((item) => item.id === id)
    : undefined;
  const startedAt = version.value?.startedAt ?? entry?.startedAt ?? '';
  return formatAbsolute(startedAt, locale.value, new Date());
});
const loading = computed(() => state.value.kind === 'loading');
const ready = computed(() => state.value.kind === 'ready');

const document = computed<Resume | null>(() => {
  const shown = version.value;
  if (shown === null) return null;
  const photo = props.record.current.document.personalDetails.photo;
  return {
    ...shown.document,
    personalDetails: {
      ...shown.document.personalDetails,
      ...(photo === undefined ? {} : { photo }),
    },
  };
});

const versionLng = computed(() => {
  const lng = version.value?.lng;
  return lng === null || lng === undefined || lng === '' ? 'und' : lng;
});
const blockedId = computed(() =>
  props.restoreBlocked ? 'history-restore-blocked' : undefined,
);

function onRestore(): void {
  if (props.restoreBlocked || !ready.value) return;
  props.controller.openRestore(props.controller.selected.value);
}

defineExpose({ focusTitle: (): void => title.value?.focus() });
</script>

<template>
  <div
    class="flex h-full min-h-0 min-w-0 flex-col"
    @keydown.esc="emit('escape')"
  >
    <div
      class="flex flex-wrap items-center gap-3 border-b border-border
        bg-background px-6 py-3 max-[42rem]:px-4"
      data-history-bar
    >
      <div
        class="min-w-[16rem] flex-1 max-[42rem]:min-w-0
          max-[42rem]:basis-full"
      >
        <h2
          ref="title"
          class="truncate text-sm font-semibold"
          tabindex="-1"
        >
          {{ copy.versionTitle(time) }}
        </h2>
        <p class="text-xs text-muted-foreground max-[42rem]:hidden">
          {{ copy.viewOnly }}
        </p>
      </div>
      <div
        class="flex items-center gap-3 max-[42rem]:w-full max-[42rem]:gap-2"
      >
        <IconButton
          class="max-[42rem]:h-10 max-[42rem]:w-10"
          data-action="history-delete"
          :disabled="loading || !ready"
          :label="copy.deleteVersionLabel"
          variant="destructive"
          @click="controller.openDelete()"
        >
          <Trash2
            :size="16"
            aria-hidden="true"
          />
        </IconButton>
        <Button
          class="max-[42rem]:h-10 max-[42rem]:flex-1"
          data-action="history-back"
          :disabled="loading"
          type="button"
          variant="outline"
          @click="emit('back')"
        >
          {{ copy.backToEditing }}
        </Button>
        <Button
          :aria-describedby="blockedId"
          :aria-disabled="restoreBlocked ? 'true' : undefined"
          class="max-[42rem]:h-10 max-[42rem]:flex-1"
          data-action="history-restore"
          :disabled="loading || !ready"
          type="button"
          @click="onRestore"
        >
          {{ copy.restore }}
        </Button>
      </div>
      <p
        v-if="restoreBlocked"
        id="history-restore-blocked"
        class="basis-full text-xs text-muted-foreground"
      >
        {{ copy.restoreBlocked }}
      </p>
    </div>
    <div class="grid min-h-0 flex-1 grid-rows-[minmax(0,1fr)]">
      <div
        v-if="loading"
        class="p-6"
      >
        <LoadingState
          :label="copy.previewLoading"
          :lines="3"
        />
      </div>
      <div
        v-else-if="state.kind === 'failed'"
        class="p-6"
      >
        <p class="text-sm">
          {{ copy.previewFailed }}
        </p>
        <Button
          class="mt-3"
          data-action="history-preview-retry"
          type="button"
          variant="outline"
          @click="controller.retryPreview()"
        >
          {{ shell.tryAgain }}
        </Button>
      </div>
      <p
        v-else-if="state.kind === 'gone'"
        class="p-6 text-sm"
      >
        {{ copy.versionGone }}
      </p>
      <EditorPreview
        v-else-if="document !== null && version !== null"
        :document="document"
        :label="copy.previewLabel(time)"
        :lng="versionLng"
        :photo-read="photoRead"
        :photo-url="photoUrl"
      />
    </div>
  </div>
</template>
