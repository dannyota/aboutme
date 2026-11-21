<script setup lang="ts">
import {
  computed,
  nextTick,
  onBeforeUnmount,
  onMounted,
  ref,
  watch,
} from 'vue';

import { Button } from '@/components/ui/button';
import { Skeleton } from '@/components/ui/skeleton';
import type { HistoryController } from '../../../editor/historyController';
import { canonicalPublicPath } from '../../../editor/publishApi';
import { editorHistoryCopy } from '../../../i18n/editor-history';
import { editorShellCopy } from '../../../i18n/editor-shell';
import type { ResumeRecord } from '../../../stores/resumes';
import StatusBanner from '../../app/StatusBanner.vue';
import InspectorPanel from '../InspectorPanel.vue';
import ConfirmDialog from '../../app/ConfirmDialog.vue';
import HistoryList from './HistoryList.vue';
import HistoryRestoreDialog from './HistoryRestoreDialog.vue';
import { formatAbsolute } from './historyFormat';

const props = defineProps<{
  readonly controller: HistoryController;
  readonly record: ResumeRecord;
  readonly restoreBlocked?: boolean;
}>();
const emit = defineEmits<{ escape: [] }>();

const { locale } = useLocale();
const copy = computed(() => editorHistoryCopy[locale.value]);
const shell = computed(() => editorShellCopy[locale.value]);
const list = ref<InstanceType<typeof HistoryList> | null>(null);
const emptyHeading = ref<HTMLElement | null>(null);

const items = computed(() =>
  props.controller.list.value.kind === 'ready'
    ? props.controller.list.value.items
    : [],
);
const ready = computed(() => props.controller.list.value.kind === 'ready');
const empty = computed(() => ready.value && items.value.length === 0);

function time(startedAt: string): string {
  return formatAbsolute(startedAt, locale.value, new Date());
}

const link = computed(() => {
  const metadata = props.record.accepted.metadata;
  const path = metadata.live ? canonicalPublicPath(metadata.slug) : null;
  return path === null ? null : `aboutme.vn${path}`;
});
const bannerText = computed(() => {
  const banner = props.controller.banner.value;
  if (banner === null) return '';
  const when = time(banner.startedAt);
  return link.value === null
    ? copy.value.restoredPrivate(when)
    : copy.value.restoredPublic(when, link.value);
});
const statusText = computed(() => {
  const status = props.controller.status.value;
  if (status === null) return '';
  if (status.kind === 'cleared') return copy.value.cleared;
  const when = time(status.startedAt);
  return status.kind === 'deleted'
    ? copy.value.deleted(when)
    : copy.value.showing(when);
});
const goneNotice = computed(
  () =>
    props.controller.preview.value.kind === 'gone'
    && props.controller.selected.value === 'current',
);
const deleteError = computed(() =>
  props.controller.remove.value?.phase === 'failed'
    ? copy.value.deleteFailed
    : undefined,
);
const clearError = computed(() =>
  props.controller.clear.value?.phase === 'failed'
    ? copy.value.clearFailed
    : undefined,
);
const deleteTime = computed(() => {
  const id = props.controller.remove.value?.id;
  const entry = items.value.find((item) => item.id === id);
  return time(entry?.startedAt ?? '');
});

function select(id: string): void {
  void props.controller.select(id);
}

function undo(): void {
  if (props.restoreBlocked === true) return;
  const id = props.controller.banner.value?.undoId;
  if (id !== null && id !== undefined) props.controller.openRestore(id);
}

watch(() => props.controller.focusRequest.value, async (request) => {
  if (request === null) return;
  await nextTick();
  if (request.kind === 'empty') emptyHeading.value?.focus();
  else {
    const id = request.kind === 'current' ? 'current' : request.id;
    void list.value?.focusOption(id);
  }
  props.controller.ackFocus();
});

watch(() => props.record.accepted.revision, () => {
  void props.controller.reload();
});
watch(() => props.record.sessionLost, (lost, was) => {
  if (was && !lost) void props.controller.reload();
});

onMounted(() => props.controller.open());
onBeforeUnmount(() => props.controller.close());
</script>

<template>
  <InspectorPanel
    :description="copy.lead"
    :title="shell.history"
    title-id="history-title"
  >
    <template
      v-if="ready && !empty"
      #heading-end
    >
      <span
        :aria-label="copy.countName(items.length)"
        class="text-sm text-muted-foreground"
        data-history-count
        role="img"
      >{{ copy.count(items.length) }}</span>
    </template>

    <StatusBanner
      v-if="controller.banner.value !== null"
      :key="controller.banner.value.id"
      data-history-restored
      focus-on-mount
      kind="success"
    >
      {{ bannerText }}
      <Button
        class="ml-2"
        data-action="history-undo"
        :aria-disabled="restoreBlocked ? 'true' : undefined"
        :disabled="controller.banner.value.undoId === null"
        size="sm"
        type="button"
        variant="outline"
        @click="undo"
      >
        {{ copy.undo }}
      </Button>
    </StatusBanner>
    <StatusBanner
      v-if="controller.loadFailed.value && ready"
      kind="error"
    >
      {{ copy.loadFailed }}
      <Button
        class="ml-2"
        data-action="history-retry"
        size="sm"
        type="button"
        variant="outline"
        @click="controller.reload()"
      >
        {{ shell.tryAgain }}
      </Button>
    </StatusBanner>
    <StatusBanner
      v-if="goneNotice"
      kind="error"
    >
      {{ copy.versionGone }}
    </StatusBanner>
    <p
      aria-live="polite"
      class="sr-only"
      data-history-status
      role="status"
    >
      {{ statusText }}
    </p>

    <div
      v-if="controller.list.value.kind === 'loading'"
      class="grid gap-1"
    >
      <span class="sr-only">{{ copy.loading }}</span>
      <Skeleton
        v-for="n in 3"
        :key="n"
        class="h-16 rounded-[10px]"
        data-history-skeleton
      />
    </div>
    <StatusBanner
      v-else-if="controller.list.value.kind === 'failed'"
      kind="error"
    >
      {{ copy.loadFailed }}
      <Button
        class="ml-2"
        data-action="history-retry"
        size="sm"
        type="button"
        variant="outline"
        @click="controller.reload()"
      >
        {{ shell.tryAgain }}
      </Button>
    </StatusBanner>
    <section
      v-else-if="empty"
      class="rounded-[10px] border border-border p-4"
    >
      <h3
        ref="emptyHeading"
        class="text-sm font-semibold"
        tabindex="-1"
      >
        {{ copy.emptyTitle }}
      </h3>
      <p class="mt-1 text-sm text-muted-foreground">
        {{ copy.emptyBody }}
      </p>
    </section>
    <template v-else>
      <HistoryList
        ref="list"
        :items="items"
        :selected="controller.selected.value"
        @escape="emit('escape')"
        @select="select"
      />
      <p class="mt-6 text-xs text-muted-foreground">
        {{ copy.retention }}
      </p>
      <div class="mt-4 border-t border-border pt-4">
        <Button
          class="h-9"
          data-action="open-clear"
          type="button"
          variant="destructive"
          @click="controller.openClear()"
        >
          {{ copy.clear }}
        </Button>
      </div>
    </template>

    <HistoryRestoreDialog
      :controller="controller"
      :record="record"
    />
    <ConfirmDialog
      :busy="controller.remove.value?.phase === 'busy'"
      :busy-label="copy.deleting"
      :cancel-label="copy.cancel"
      class="max-w-[32rem] sm:max-w-[32rem]"
      confirm-action="delete-confirm"
      :confirm-label="copy.deleteConfirm"
      :description="copy.deleteBody"
      destructive
      :error="deleteError"
      :open="controller.remove.value !== null"
      :title="copy.deleteTitle(deleteTime)"
      @cancel="controller.closeDelete()"
      @confirm="controller.confirmDelete()"
    />
    <ConfirmDialog
      :busy="controller.clear.value?.phase === 'busy'"
      :busy-label="copy.clearing"
      :cancel-label="copy.cancel"
      class="max-w-[32rem] sm:max-w-[32rem]"
      confirm-action="clear-confirm"
      :confirm-label="copy.clearConfirm"
      :description="copy.clearBody(items.length)"
      destructive
      :error="clearError"
      :open="controller.clear.value !== null"
      :title="copy.clearTitle"
      @cancel="controller.closeClear()"
      @confirm="controller.confirmClear()"
    />
  </InspectorPanel>
</template>
