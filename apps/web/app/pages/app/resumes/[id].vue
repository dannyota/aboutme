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
const codec: PhotoDataCodec = { toDataURL: bytesToDataURL };
const photo = createPhotoController({ api, store, codec });

useUnsavedNavigationGuard(record);

async function load(): Promise<void> {
  if (loadingStarted.value || id === '') return;
  loadingStarted.value = true;
  const result = await api.read(id);
  switch (result.kind) {
    case 'complete':
      store.initialize(result.accepted);
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
  }
}

function initializeActions(): void {
  if (actions.value !== undefined) return;
  actions.value = useResumeEditor(id);
}

onMounted(() => {
  watch(
    auth.authState,
    async (state) => {
      if (state === 'authenticated') {
        initializeActions();
        await load();
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

onBeforeUnmount(() => photo.clear());

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
      loadState === 'ready'
        && record !== undefined
        && actions !== undefined
    "
    :actions="actions"
    :record="record"
  />
  <main
    v-else
    class="editor-route-state"
  >
    <p
      v-if="loadState === 'loading'"
      role="status"
    >
      Loading editor…
    </p>
    <template v-else-if="loadState === 'unavailable'">
      <h1>Resume unavailable</h1>
      <p>This resume is not available.</p>
      <NuxtLink to="/app/resumes"> Back to resumes </NuxtLink>
    </template>
    <template v-else>
      <h1>Editor unavailable</h1>
      <p>We could not open this resume. Try again.</p>
      <button
        type="button"
        @click="
          loadingStarted = false;
          loadState = 'loading';
          load();
        "
      >
        Try again
      </button>
    </template>
  </main>
</template>
