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
import {
  createFieldDrafts,
  FieldDraftsKey,
} from '../../../composables/useFieldDrafts';
import { createSessionCheck } from '../../../composables/sessionCheck';
import type { AuthState } from '../../../composables/useAuth';
import { pageTitle, workspaceTitles } from '@/i18n/meta';
import { editorShellCopy } from '@/i18n/editor-shell';

type LoadState = 'loading' | 'ready' | 'unavailable' | 'failed';

const route = useRoute();
const auth = useAuth();
const store = useResumeStore();
const api = createResumeApi();
const resumeId = route.params.id;
const id = typeof resumeId === 'string' ? resumeId : (resumeId?.[0] ?? '');
const actions = shallowRef<ResumeEditorActions>();
const record = computed(() => actions.value?.record.value);
const { locale } = useLocale();
const copy = computed(() => editorShellCopy[locale.value]);
useHead(computed(() => {
  const title = record.value?.current.metadata.title.trim();
  return {
    title: title ? pageTitle(title) : workspaceTitles[locale.value].editor,
  };
}));
const loadState = ref<LoadState>('loading');
const loadingStarted = ref(false);
const parentRevisionETag = ref<ParentETag>();
let realtime: ReturnType<typeof createRealtimeController> | null = null;
const codec: PhotoDataCodec = { toDataURL: bytesToDataURL };
const photo = createPhotoController({ api, store, codec });

const drafts = createFieldDrafts();
provide(FieldDraftsKey, drafts);
useUnsavedNavigationGuard(record, drafts);
const sessionCheck = createSessionCheck({
  check: () => auth.refresh(),
  authState: auth.authState,
  retryAfterMs: () => auth.retryAfterMs.value,
});
/** The verified owner of the editor's work, kept for the page's lifetime. */
let owner: string | undefined;
/** Since its owner's last answer the session was in doubt or not theirs. */
let recovering = false;
/** The page closed: a read it started no longer touches the store. */
let disposed = false;
/** Counts the session's answers; a newer one supersedes an older one. */
let answers = 0;

function stopRealtime(): void {
  realtime?.stop();
  realtime = null;
}

async function load(): Promise<void> {
  if (loadingStarted.value || id === '') return;
  loadingStarted.value = true;
  const result = await api.read(id);
  // A page that closed meanwhile leaves the store to the one now open.
  if (disposed) return;
  switch (result.kind) {
    case 'complete':
      // Nothing re-initializes a record that holds unsent or in-flight work:
      // the store refuses, and the page adopts the read into that record
      // instead. This page's first complete read is its only initialize;
      // once the editor is open, a failed session check keeps the record
      // and checks again instead of reading it afresh. Closing the editor
      // drops its unsent work, so a resume opened again starts afresh.
      if (!store.initialize(result.accepted)) {
        store.adoptCompleteRead(id, result.accepted);
      }
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

/** Opens the editor's actions for its verified owner, once. */
function openActions(ownerId: string): void {
  if (actions.value !== undefined) return;
  actions.value = useResumeEditor(id, undefined, {
    ownerId,
    // Resume after sign-in asks for an answer; settleSession acts on it.
    recheckSession: () => auth.refresh(),
  });
}

/**
 * The one place an answer about the session acts, whether or not an earlier
 * check failed; nothing else starts or stops the session check or starts
 * realtime. The owner's session opens or wakes the editor; an error keeps
 * the open editor and checks again; any other answer, signed out or another
 * person, holds the editor's work for its owner and sends none of it. Its
 * one await is the first load: if a newer answer came meanwhile, the opened
 * editor settles against the session as it is now instead.
 */
async function settleSession(state: AuthState): Promise<void> {
  const answer = ++answers;
  // A check in flight passes through `loading`; only an answer acts.
  if (state === 'loading') return;
  if (state === 'error') {
    stopRealtime();
    recovering = true;
    // Only a first load shows Editor unavailable.
    if (loadState.value === 'ready') sessionCheck.start();
    else loadState.value = 'failed';
    return;
  }
  sessionCheck.stop();
  const userId = state === 'authenticated' ? auth.user.value?.id : undefined;
  if (userId !== undefined && (owner === undefined || userId === owner)) {
    owner = userId;
    openActions(userId);
    if (loadState.value === 'ready') {
      // Every drain that stopped for authentication goes on; after doubt or
      // a lost session the queue reads the resume first.
      const recovered = recovering || record.value?.sessionLost === true;
      void actions.value?.wake(recovered);
    } else {
      await load();
      if (disposed) return;
      if (answer !== answers) {
        await settleSession(auth.authState.value);
        return;
      }
    }
    recovering = false;
    if (loadState.value === 'ready') startRealtime();
    return;
  }
  stopRealtime();
  recovering = true;
  if (shouldRetainEditorOnSessionLoss(record.value)) {
    store.markSessionLost(id);
    loadState.value = 'ready';
    return;
  }
  // A first-visit anonymous state is the shared route guard's job
  // (middleware/signed-in.global.ts); only a session that changed after
  // this page opened is this page's own to send on.
  if (owner !== undefined) await navigateTo('/login');
}

/**
 * Try again under Editor unavailable: the session is checked again, and its
 * answer opens the editor's actions and loads the resume.
 */
function retryFirstLoad(): void {
  loadingStarted.value = false;
  loadState.value = 'loading';
  void auth.refresh();
}

onMounted(() => {
  watch(auth.authState, (state) => void settleSession(state), {
    immediate: true,
  });
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
  disposed = true;
  stopRealtime();
  photo.clear();
  sessionCheck.stop();
  actions.value?.stop();
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
    :session-check="sessionCheck"
  />
  <main
    v-else
    class="grid min-h-dvh place-content-center bg-background p-8 text-center
      text-foreground"
  >
    <LoadingState
      v-if="loadState === 'loading'"
      class="mx-auto w-full max-w-md"
      :label="copy.loadingEditor"
    />
    <template v-else-if="loadState === 'unavailable'">
      <EmptyState
        :title="copy.resumeUnavailable"
        :description="copy.resumeUnavailableDescription"
      >
        <template #action>
          <NuxtLink
            :class="buttonVariants({ variant: 'outline' })"
            to="/app/resumes"
          >
            {{ copy.backToResumes }}
          </NuxtLink>
        </template>
      </EmptyState>
    </template>
    <template v-else>
      <EmptyState
        :title="copy.editorUnavailable"
        :description="copy.editorUnavailableDescription"
      >
        <template #action>
          <Button
            type="button"
            @click="retryFirstLoad"
          >
            {{ copy.tryAgain }}
          </Button>
        </template>
      </EmptyState>
    </template>
  </main>
</template>
