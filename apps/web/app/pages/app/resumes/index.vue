<script setup lang="ts">
import type { ResumeSummary } from '../../../editor/resumeApi';
import type { OpaqueCreateOutcome } from '../../../editor/coordinator';
import {
  createStatusMessage,
  useResumeList,
} from '../../../composables/useResumeList';

const list = useResumeList();
const createOpen = ref(false);
const renameItem = ref<ResumeSummary | null>(null);
const deleteItem = ref<ResumeSummary | null>(null);
const retained = ref<OpaqueCreateOutcome | null>(null);
const busyIds = ref(new Set<string>());
const createBusy = ref(false);
const createMessage = ref<string | null>(null);

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
): Promise<void> {
  createBusy.value = true;
  createMessage.value = null;
  const result = await list.create(title, lng);
  createBusy.value = false;
  if (result.kind === 'opaque-create') retained.value = result.outcome;
  createMessage.value = createStatusMessage(result);
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
  <main class="app-page resume-list-page">
    <p
      v-if="list.view.value.kind === 'waiting-auth'"
      role="status"
    >
      Checking your session.
    </p>
    <p
      v-else-if="list.view.value.kind === 'loading'"
      role="status"
    >
      Loading resumes.
    </p>
    <p
      v-else-if="list.view.value.kind === 'unavailable'"
      role="alert"
    >
      Resumes are unavailable. Try again.
    </p>
    <EditorListResumeList
      v-else
      :items="list.items.value"
      :busy-ids="[...busyIds]"
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
    <p
      v-if="createMessage !== null || list.actionMessage.value !== null"
      role="status"
    >
      {{ createMessage ?? list.actionMessage.value }}
    </p>
  </main>
</template>
