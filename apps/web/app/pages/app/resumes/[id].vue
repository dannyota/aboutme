<script setup lang="ts">
import {
  computed,
  onBeforeUnmount,
  onMounted,
  ref,
  shallowRef,
  toRaw,
  watch,
} from 'vue';

import EditorShell from '../../../components/editor/EditorShell.vue';
import EmptyState from '../../../components/app/EmptyState.vue';
import LoadingState from '../../../components/app/LoadingState.vue';
import { Button, buttonVariants } from '@/components/ui/button';
import {
  useResumeEditor,
  type ResumeEditorActions,
} from '../../../composables/useResumeEditor';
import {
  shouldRetainEditorOnSessionLoss,
  useUnsavedNavigationGuard,
} from '../../../composables/useUnsavedNavigationGuard';
import {
  createPhotoController,
  type PhotoDataCodec,
} from '../../../editor/photoController';
import { createResumeApi } from '../../../editor/resumeApi';
import {
  compareRevision,
  parentETag,
} from '../../../editor/revision';
import {
  createRealtimeController,
  type RealtimeReadResult,
} from '../../../realtime/controller';
import type { ParentETag } from '../../../editor/types';
import { ownerRevisionDecision } from '../../../realtime/owner';
import { useResumeStore } from '../../../stores/resumes';

type LoadState = 'loading' | 'ready' | 'unavailable' | 'failed';

const route = useRoute();
const auth = useAuth();
const store = useResumeStore();
const api = createResumeApi();
const resumeId = route.params.id;
const id = typeof resumeId === 'string' ? resumeId : (resumeId?.[0] ?? '');
const actions = shallowRef<ResumeEditorActions>();
const record = computed(() => actions.value?.record.value);
const loadState = ref<LoadState>('loading');
const loadingStarted = ref(false);
const parentRevisionETag = ref<ParentETag>();
let realtime: ReturnType<typeof createRealtimeController> | null = null;
const codec: PhotoDataCodec = { toDataURL: bytesToDataURL };
const photo = createPhotoController({ api, store, codec });

useUnsavedNavigationGuard(record);

function stopRealtime(): void {
  realtime?.stop();
  realtime = null;
}

async function load(): Promise<void> {
  if (loadingStarted.value || id === '') return;
  loadingStarted.value = true;
  const result = await api.read(id);
  switch (result.kind) {
    case 'complete':
      store.initialize(result.accepted);
      parentRevisionETag.value = parentETag(result.accepted.revision);
      loadState.value = 'ready';
      break;
    case 'unavailable':
      loadState.value = 'unavailable';
      break;
    case 'session-lost':
      await navigateTo('/login');
      break;
    case 'rate-limited':
    case 'failed':
      loadState.value = 'failed';
      break;
    case 'unknown-version':
      window.location.reload();
      break;
  }
}

async function realtimeRead(
  mode: 'unconditional' | 'conditional',
): Promise<RealtimeReadResult> {
  const currentActions = actions.value;
  if (currentActions === undefined) return { kind: 'failed' };
  if (mode === 'unconditional') {
    const previousRevision = record.value?.accepted.revision;
    const result = await currentActions.refresh();
    if (result.kind === 'complete') {
      parentRevisionETag.value = parentETag(result.accepted.revision);
      if (previousRevision !== undefined) {
        const comparison = compareRevision(
          result.accepted.revision,
          previousRevision,
        );
        if (comparison < 0) return { kind: 'failed' };
        if (comparison === 0) return { kind: 'unchanged' };
      }
      return { kind: 'updated' };
    }
    if (result.kind === 'unavailable') return { kind: 'not-found' };
    if (result.kind === 'session-lost') {
      stopRealtime();
      return { kind: 'failed' };
    }
    if (result.kind === 'unknown-version') return { kind: 'unknown-version' };
    return { kind: 'failed' };
  }
  const previousRevision = record.value?.accepted.revision;
  const result = await currentActions.refreshConditional?.(
    parentRevisionETag.value,
  );
  if (result === undefined) return { kind: 'failed' };
  if (result.kind === 'complete') {
    parentRevisionETag.value = result.etag;
    if (previousRevision !== undefined) {
      const comparison = compareRevision(
        result.accepted.revision,
        previousRevision,
      );
      if (comparison < 0) return { kind: 'failed' };
      if (comparison === 0) return { kind: 'unchanged' };
    }
    return { kind: 'updated' };
  }
  if (result.kind === 'not-modified') {
    parentRevisionETag.value = result.etag;
    return { kind: 'unchanged' };
  }
  if (result.kind === 'unavailable') return { kind: 'not-found' };
  if (result.kind === 'session-lost') {
    stopRealtime();
    return { kind: 'failed' };
  }
  if (result.kind === 'unknown-version') return { kind: 'unknown-version' };
  return { kind: 'failed' };
}

function startRealtime(): void {
  if (realtime !== null || id === '') return;
  realtime = createRealtimeController({
    url: '/api/v1/events',
    withCredentials: true,
    refetch: realtimeRead,
    onRevision: (value) => ownerRevisionDecision(
      value,
      id,
      record.value?.accepted.revision,
    ),
    onUnknownVersion: () => window.location.reload(),
    onNotFound: () => {
      loadState.value = 'unavailable';
    },
  });
  realtime.start();
}

function initializeActions(): void {
  if (actions.value !== undefined) return;
  actions.value = useResumeEditor(id);
}

onMounted(() => {
  watch(
    auth.authState,
    async (state) => {
      if (state !== 'authenticated') stopRealtime();
      if (state === 'authenticated') {
        initializeActions();
        await load();
        if (loadState.value === 'ready') startRealtime();
      }
      if (state === 'anonymous') {
        if (shouldRetainEditorOnSessionLoss(record.value)) {
          store.markSessionLost(id);
          loadState.value = 'ready';
          return;
        }
        await navigateTo('/login');
      }
      if (state === 'error') loadState.value = 'failed';
    },
    { immediate: true },
  );
});

watch(
  () => {
    const accepted = record.value?.accepted;
    return accepted === undefined
      ? ''
      : [
          accepted.revision,
          accepted.document.personalDetails.photo?.key ?? '',
        ].join(':');
  },
  async (next, previous) => {
    if (next === '' || next === previous) return;
    const accepted = record.value?.accepted;
    if (accepted !== undefined) await photo.sync(toRaw(accepted));
  },
  { flush: 'post' },
);

onBeforeUnmount(() => {
  stopRealtime();
  photo.clear();
});

async function bytesToDataURL(
  bytes: Uint8Array,
  mime: 'image/jpeg' | 'image/png',
): Promise<string> {
  let binary = '';
  const chunkBytes = 8_192;
  for (let offset = 0; offset < bytes.length; offset += chunkBytes) {
    const chunk = bytes.subarray(offset, offset + chunkBytes);
    binary += String.fromCharCode(...chunk);
  }
  return `data:${mime};base64,${window.btoa(binary)}`;
}
</script>

<template>
  <EditorShell
    v-if="
      loadState === 'ready' && record !== undefined && actions !== undefined
    "
    :actions="actions"
    :record="record"
  />
  <main
    v-else
    class="grid min-h-dvh place-content-center bg-background p-8 text-center
      text-foreground"
  >
    <LoadingState
      v-if="loadState === 'loading'"
      class="mx-auto w-full max-w-md"
      label="Loading editor"
    />
    <template v-else-if="loadState === 'unavailable'">
      <EmptyState
        title="Resume unavailable"
        description="This resume is not available."
      >
        <template #action>
          <NuxtLink
            :class="buttonVariants({ variant: 'outline' })"
            to="/app/resumes"
          >
            Back to resumes
          </NuxtLink>
        </template>
      </EmptyState>
    </template>
    <template v-else>
      <EmptyState
        title="Editor unavailable"
        description="We could not open this resume. Try again."
      >
        <template #action>
          <Button
            type="button"
            @click="
              loadingStarted = false;
              loadState = 'loading';
              load();
            "
          >
            Try again
          </Button>
        </template>
      </EmptyState>
    </template>
  </main>
</template>
