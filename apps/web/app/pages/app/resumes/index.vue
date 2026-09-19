<script setup lang="ts">
import type { Resume } from '@aboutme/schema';
import type { ResumeSummary } from '../../../editor/resumeApi';
import type { OpaqueCreateOutcome } from '../../../editor/coordinator';
import type { CreateNotice } from '../../../composables/useResumeList';
import LoadingState from '@/components/app/LoadingState.vue';
import StatusBanner from '@/components/app/StatusBanner.vue';
import {
  createNotice,
  useResumeList,
} from '../../../composables/useResumeList';
import { useNow } from '../../../composables/useNow';
import { workspaceTitles } from '@/i18n/meta';
import { resumeListCopy } from '@/i18n/resume-list';

const { locale } = useLocale();
const copy = computed(() => resumeListCopy[locale.value]);

useHead({ title: computed(() => workspaceTitles[locale.value].resumes) });

const list = useResumeList();
const now = useNow();
const createOpen = ref(false);
const renameItem = ref<ResumeSummary | null>(null);
const deleteItem = ref<ResumeSummary | null>(null);
const retained = ref<OpaqueCreateOutcome | null>(null);
const busyIds = ref(new Set<string>());
const createBusy = ref(false);
const createNoticeCode = ref<CreateNotice>(null);

const createMessage = computed(() => {
  switch (createNoticeCode.value) {
    case 'resume-cap': return copy.value.resumeCap;
    case 'create-failed': return copy.value.createFailed;
    case 'retry-later': return copy.value.retryLater;
    case 'session-lost': return copy.value.sessionLost;
    default: return null;
  }
});
const actionMessage = computed(() =>
  list.actionNotice.value === 'resume-changed'
    ? copy.value.resumeChanged
    : null);

function begin(id: string): void {
  busyIds.value = new Set([...busyIds.value, id]);
}

function end(id: string): void {
  const next = new Set(busyIds.value);
  next.delete(id);
  busyIds.value = next;
}

async function create(
  title: string,
  lng: string | null | undefined,
  document?: Resume,
): Promise<void> {
  createBusy.value = true;
  createNoticeCode.value = null;
  const result = await list.create(title, lng, document);
  createBusy.value = false;
  if (result.kind === 'opaque-create') retained.value = result.outcome;
  createNoticeCode.value = createNotice(result);
  if (result.kind === 'created') {
    createOpen.value = false;
  }
}

async function refreshCreate(intentId: string): Promise<void> {
  createBusy.value = true;
  retained.value = await list.refreshCreate(intentId);
  createBusy.value = false;
}

function abandonCreate(intentId: string): void {
  list.abandonCreate(intentId);
  retained.value = null;
}

async function rename(id: string, title: string): Promise<void> {
  begin(id);
  await list.rename(id, title);
  end(id);
  renameItem.value = null;
}

async function remove(id: string, title: string): Promise<void> {
  begin(id);
  await list.remove(id, title);
  end(id);
  deleteItem.value = null;
}
</script>

<template>
  <main class="app-page space-y-6">
    <LoadingState
      v-if="list.view.value.kind === 'waiting-auth'"
      :label="copy.waitingAuth"
    />
    <LoadingState
      v-else-if="list.view.value.kind === 'loading'"
      :label="copy.loading"
    />
    <StatusBanner
      v-else-if="list.view.value.kind === 'unavailable'"
      kind="error"
    >
      {{ copy.unavailable }}
    </StatusBanner>
    <EditorListResumeList
      v-else
      :items="list.items.value"
      :busy-ids="[...busyIds]"
      :now="now"
      :removal-focus-id="list.removalFocusId.value"
      :removal-focus-version="list.removalFocusVersion.value"
      @create="createOpen = true"
      @rename="renameItem = $event"
      @remove="deleteItem = $event"
    />
    <EditorListCreateResumeDialog
      :open="createOpen"
      :busy="createBusy"
      :retained="retained"
      :resume-count="list.items.value.length"
      @close="createOpen = false"
      @submit="create"
      @refresh="refreshCreate"
      @abandon="abandonCreate"
    />
    <EditorListRenameResumeDialog
      :item="renameItem"
      :busy="renameItem !== null && busyIds.has(renameItem.id)"
      @close="renameItem = null"
      @submit="rename"
    />
    <EditorListDeleteResumeDialog
      :item="deleteItem"
      :busy="deleteItem !== null && busyIds.has(deleteItem.id)"
      @close="deleteItem = null"
      @submit="remove"
    />
    <StatusBanner
      v-if="createMessage !== null || actionMessage !== null"
      kind="info"
    >
      {{ createMessage ?? actionMessage }}
    </StatusBanner>
  </main>
</template>
