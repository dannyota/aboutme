<script setup lang="ts">
import { computed, onMounted, ref } from 'vue';

import { Button } from '@/components/ui/button';
import { useAgentGrants } from '../../composables/agentGrants';
import {
  dismissLanguageTwin,
  readLanguageTwin,
  type TwinNote,
} from '../../editor/languageTwin';
import { languageTwinCopy } from '@/i18n/language-twin';

const props = defineProps<{
  readonly resumeId: string;
  readonly title: string;
}>();

const { locale } = useLocale();
const copy = computed(() => languageTwinCopy[locale.value]);
const { grants, refresh } = useAgentGrants();
const stored = ref<ReturnType<typeof readLanguageTwin>>(null);
const agentReady = ref(false);
const agentChecked = ref(false);
const copied = ref(false);
const promptVisible = ref(false);

const lines = computed(() => {
  const review = stored.value;
  if (review === null) return [];
  const text = copy.value;
  const target = review.target;
  const notes = new Set<TwinNote>(review.notes);
  const items = [
    text.sentences[target],
    text.numbers[target],
  ];
  if (notes.has('photo-removed')) items.push(text.photoRemoved[target]);
  if (notes.has('birth-removed')) items.push(text.birthRemoved);
  if (notes.has('zalo-added')) items.push(text.zaloAdded);
  if (notes.has('zalo-full')) items.push(text.zaloFull);
  if (notes.has('zalo-removed')) items.push(text.zaloRemoved);
  if (notes.has('no-phone')) items.push(text.noPhone);
  if (notes.has('headings-left')) items.push(text.headingsLeft);
  items.push(text.unpublished);
  return items;
});

const prompt = computed(() => {
  const review = stored.value;
  if (review === null) return '';
  return copy.value.agentPrompt(
    props.title,
    copy.value.languageName[review.target],
  );
});

onMounted(async () => {
  stored.value = readLanguageTwin(props.resumeId);
  if (stored.value === null) return;
  try {
    await refresh();
    agentReady.value = grants.value.some((grant) =>
      grant.scopes.includes('resumes:write'),
    );
  } catch {
    agentReady.value = false;
  }
  agentChecked.value = true;
});

async function copyPrompt(): Promise<void> {
  try {
    await navigator.clipboard.writeText(prompt.value);
    copied.value = true;
    promptVisible.value = false;
  } catch {
    copied.value = false;
    promptVisible.value = true;
  }
}

function dismiss(): void {
  dismissLanguageTwin(props.resumeId);
  stored.value = null;
}
</script>

<template>
  <aside
    v-if="stored !== null"
    class="fixed inset-x-3 bottom-3 z-40 max-h-[50vh] max-w-lg
      overflow-auto rounded-md border bg-card p-4 text-sm
      sm:inset-x-auto sm:right-4 sm:w-96"
    data-testid="language-twin-review"
    :aria-labelledby="`language-twin-${resumeId}`"
  >
    <h2
      :id="`language-twin-${resumeId}`"
      class="text-base font-semibold"
    >
      {{ copy.title }}
    </h2>
    <ul class="mt-3 list-disc space-y-2 ps-5">
      <li
        v-for="(line, index) in lines"
        :key="index"
      >
        {{ line }}
      </li>
    </ul>
    <p
      v-if="agentChecked"
      class="mt-3"
    >
      {{ agentReady ? copy.agentReady : copy.agentAbsent }}
    </p>
    <p
      v-if="agentChecked && !agentReady"
      class="mt-2"
    >
      <NuxtLink
        class="text-link underline-offset-4 hover:underline"
        to="/app/settings/sessions"
      >
        {{ copy.settings }}
      </NuxtLink>
    </p>
    <pre
      v-if="promptVisible"
      class="mt-3 overflow-auto whitespace-pre-wrap rounded-md
        border bg-muted p-3"
    >{{ prompt }}</pre>
    <div class="mt-4 flex flex-wrap gap-2">
      <Button
        size="sm"
        type="button"
        @click="copyPrompt"
      >
        {{ copied ? copy.copied : copy.copyInstructions }}
      </Button>
      <Button
        size="sm"
        type="button"
        variant="outline"
        @click="dismiss"
      >
        {{ copy.dismiss }}
      </Button>
    </div>
  </aside>
</template>
